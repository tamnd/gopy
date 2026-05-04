// Package hash holds the hashing primitives ported from
// cpython/Python/pyhash.c and cpython/Python/bootstrap_hash.c.
//
// In v0.1 only the secret-bootstrap half is implemented. The full
// SipHash-1-3, FNV, and x86_aes hashers land with pyhash.c in v0.4.
package hash

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
)

// SecretSize is the size of the hash secret. Matches
// sizeof(_Py_HashSecret_t) in CPython 3.14: 16 bytes of SipHash key
// followed by 8 bytes of FNV salt.
const SecretSize = 24

// Secret is the per-process hash secret. v0.4 will read it from the
// hashers; until then, callers only seed it.
var Secret [SecretSize]byte

// SecretMode classifies how Init resolved the hash secret.
type SecretMode int

const (
	// SecretRandom means the secret was filled from OS entropy.
	SecretRandom SecretMode = iota
	// SecretZeroed means PYTHONHASHSEED=0 was honored: the secret is
	// all zero so hashes are deterministic for debugging.
	SecretZeroed
	// SecretSeeded means PYTHONHASHSEED was a positive integer and the
	// secret was filled deterministically via lcg_urandom.
	SecretSeeded
)

// Config carries the subset of PyConfig that bootstrap_hash.c reads.
// The full PyConfig lands in v0.7 (initconfig).
type Config struct {
	UseHashSeed bool
	HashSeed    uint32
}

// ErrInvalidSeed is returned by Init when PYTHONHASHSEED is set to a
// value that is neither "random", "0", nor a non-negative 32-bit
// integer.
var ErrInvalidSeed = errors.New("hash: PYTHONHASHSEED must be \"random\", \"0\", or a non-negative integer in [1, 4294967295]")

var (
	initOnce sync.Once
	mode     SecretMode
)

// Init seeds Secret. If cfg is nil, the PYTHONHASHSEED environment
// variable is consulted. Init is safe to call concurrently; only the
// first call performs work, matching CPython's
// _Py_HashSecret_Initialized guard.
func Init(cfg *Config) (SecretMode, error) {
	var firstErr error
	initOnce.Do(func() {
		c, err := resolveConfig(cfg)
		if err != nil {
			firstErr = err
			return
		}
		switch {
		case !c.UseHashSeed:
			if _, err := rand.Read(Secret[:]); err != nil {
				firstErr = fmt.Errorf("hash: read OS entropy: %w", err)
				return
			}
			mode = SecretRandom
		case c.HashSeed == 0:
			for i := range Secret {
				Secret[i] = 0
			}
			mode = SecretZeroed
		default:
			lcgURandom(c.HashSeed, Secret[:])
			mode = SecretSeeded
		}
	})
	return mode, firstErr
}

// Reset clears the init guard. Tests use this to re-seed the secret;
// production code must not call it.
func Reset() {
	initOnce = sync.Once{}
	for i := range Secret {
		Secret[i] = 0
	}
	mode = SecretRandom
}

// resolveConfig derives a Config from cfg if non-nil, otherwise from
// the PYTHONHASHSEED environment variable.
func resolveConfig(cfg *Config) (Config, error) {
	if cfg != nil {
		return *cfg, nil
	}
	v, ok := os.LookupEnv("PYTHONHASHSEED")
	if !ok || v == "" || v == "random" {
		return Config{}, nil
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return Config{}, ErrInvalidSeed
	}
	return Config{UseHashSeed: true, HashSeed: uint32(n)}, nil
}

// lcgURandom fills out with the byte stream defined in
// cpython/Python/bootstrap_hash.c::lcg_urandom. The sequence is a
// linear congruential generator using the Microsoft-CRT constants
// 214013 and 2531011, taking bits 16..23 of each step. The output
// is byte-identical to CPython's for every seed.
func lcgURandom(x0 uint32, out []byte) {
	x := x0
	for i := range out {
		x = x*214013 + 2531011
		out[i] = byte((x >> 16) & 0xff)
	}
}
