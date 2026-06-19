//go:build windows

package vm

// winerrorToErrno maps a Windows system error code to the POSIX errno
// CPython stores in OSError.errno (OSError.winerror keeps the raw code).
// Go's syscall.Errno on Windows carries the raw WinAPI error, so without
// this translation `except FileNotFoundError` / `except FileExistsError`
// would never match (errnomap is keyed on POSIX errno).
//
// CPython: PC/errmap.h winerror_to_errno
func winerrorToErrno(winerror int) int {
	// Unwrap FACILITY_WIN32 HRESULT errors.
	if winerror&0xFFFF0000 == 0x80070000 {
		winerror &= 0x0000FFFF
	}

	// Winsock error codes (10000-11999) are errno values.
	if winerror >= 10000 && winerror < 12000 {
		switch winerror {
		case 10004, // WSAEINTR
			10009, // WSAEBADF
			10013, // WSAEACCES
			10014, // WSAEFAULT
			10022, // WSAEINVAL
			10024: // WSAEMFILE
			return winerror - 10000
		default:
			return winerror
		}
	}

	switch winerror {
	case 2, // ERROR_FILE_NOT_FOUND
		3,   // ERROR_PATH_NOT_FOUND
		15,  // ERROR_INVALID_DRIVE
		18,  // ERROR_NO_MORE_FILES
		53,  // ERROR_BAD_NETPATH
		67,  // ERROR_BAD_NET_NAME
		161, // ERROR_BAD_PATHNAME
		206: // ERROR_FILENAME_EXCED_RANGE
		return errENOENT
	case 10: // ERROR_BAD_ENVIRONMENT
		return errE2BIG
	case 11, // ERROR_BAD_FORMAT
		188, // ERROR_INVALID_STARTING_CODESEG
		189, // ERROR_INVALID_STACKSEG
		190, // ERROR_INVALID_MODULETYPE
		191, // ERROR_INVALID_EXE_SIGNATURE
		192, // ERROR_EXE_MARKED_INVALID
		193, // ERROR_BAD_EXE_FORMAT
		194, // ERROR_ITERATED_DATA_EXCEEDS_64k
		195, // ERROR_INVALID_MINALLOCSIZE
		196, // ERROR_DYNLINK_FROM_INVALID_RING
		197, // ERROR_IOPL_NOT_ENABLED
		198, // ERROR_INVALID_SEGDPL
		199, // ERROR_AUTODATASEG_EXCEEDS_64k
		200, // ERROR_RING2SEG_MUST_BE_MOVABLE
		201, // ERROR_RELOC_CHAIN_XEEDS_SEGLIM
		202: // ERROR_INFLOOP_IN_RELOC_CHAIN
		return errENOEXEC
	case 6, // ERROR_INVALID_HANDLE
		114, // ERROR_INVALID_TARGET_HANDLE
		130: // ERROR_DIRECT_ACCESS_HANDLE
		return errEBADF
	case 128, // ERROR_WAIT_NO_CHILDREN
		129: // ERROR_CHILD_NOT_COMPLETE
		return errECHILD
	case 89, // ERROR_NO_PROC_SLOTS
		164, // ERROR_MAX_THRDS_REACHED
		215: // ERROR_NESTING_NOT_ALLOWED
		return errEAGAIN
	case 7, // ERROR_ARENA_TRASHED
		8,    // ERROR_NOT_ENOUGH_MEMORY
		9,    // ERROR_INVALID_BLOCK
		1816: // ERROR_NOT_ENOUGH_QUOTA
		return errENOMEM
	case 5, // ERROR_ACCESS_DENIED
		16,  // ERROR_CURRENT_DIRECTORY
		19,  // ERROR_WRITE_PROTECT
		20,  // ERROR_BAD_UNIT
		21,  // ERROR_NOT_READY
		22,  // ERROR_BAD_COMMAND
		23,  // ERROR_CRC
		24,  // ERROR_BAD_LENGTH
		25,  // ERROR_SEEK
		26,  // ERROR_NOT_DOS_DISK
		27,  // ERROR_SECTOR_NOT_FOUND
		28,  // ERROR_OUT_OF_PAPER
		29,  // ERROR_WRITE_FAULT
		30,  // ERROR_READ_FAULT
		31,  // ERROR_GEN_FAILURE
		32,  // ERROR_SHARING_VIOLATION
		33,  // ERROR_LOCK_VIOLATION
		34,  // ERROR_WRONG_DISK
		36,  // ERROR_SHARING_BUFFER_EXCEEDED
		65,  // ERROR_NETWORK_ACCESS_DENIED
		82,  // ERROR_CANNOT_MAKE
		83,  // ERROR_FAIL_I24
		108, // ERROR_DRIVE_LOCKED
		132, // ERROR_SEEK_ON_DEVICE
		158, // ERROR_NOT_LOCKED
		167, // ERROR_LOCK_FAILED
		35:  // 35 (undefined)
		return errEACCES
	case 80, // ERROR_FILE_EXISTS
		183: // ERROR_ALREADY_EXISTS
		return errEEXIST
	case 17: // ERROR_NOT_SAME_DEVICE
		return errEXDEV
	case 267: // ERROR_DIRECTORY (bpo-12802)
		return errENOTDIR
	case 4: // ERROR_TOO_MANY_OPEN_FILES
		return errEMFILE
	case 112: // ERROR_DISK_FULL
		return errENOSPC
	case 109, // ERROR_BROKEN_PIPE
		232: // ERROR_NO_DATA (bpo-13063)
		return errEPIPE
	case 145: // ERROR_DIR_NOT_EMPTY
		return errENOTEMPTY
	case 1113: // ERROR_NO_UNICODE_TRANSLATION
		return errEILSEQ
	case 258: // WAIT_TIMEOUT
		return errETIMEDOUT
	case 1, // ERROR_INVALID_FUNCTION
		12,  // ERROR_INVALID_ACCESS
		13,  // ERROR_INVALID_DATA
		87,  // ERROR_INVALID_PARAMETER
		131: // ERROR_NEGATIVE_SEEK
		return errEINVAL
	default:
		return errEINVAL
	}
}

// POSIX errno values CPython maps Windows errors onto. Hard-coded to the
// standard POSIX numbers (not Go's Windows-side syscall constants, which
// are fabricated) so they line up with module/errno and errnomap.
const (
	errENOENT    = 2
	errE2BIG     = 7
	errENOEXEC   = 8
	errEBADF     = 9
	errECHILD    = 10
	errEAGAIN    = 11
	errENOMEM    = 12
	errEACCES    = 13
	errEEXIST    = 17
	errEXDEV     = 18
	errENOTDIR   = 20
	errEINVAL    = 22
	errEMFILE    = 24
	errENOSPC    = 28
	errEPIPE     = 32
	errENOTEMPTY = 41
	errEILSEQ    = 42
	errETIMEDOUT = 138
)
