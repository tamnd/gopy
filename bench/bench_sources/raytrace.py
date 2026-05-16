# raytrace — minimal sphere ray tracer. Float-heavy, no output.
import os, math
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def sub(a, b): return (a[0]-b[0], a[1]-b[1], a[2]-b[2])
def dot(a, b): return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]

def trace(origin, direction, spheres):
    closest_t = 1e30
    hit = -1
    for i, s in enumerate(spheres):
        oc = sub(origin, s[0])
        b = dot(oc, direction)
        c = dot(oc, oc) - s[1] * s[1]
        disc = b * b - c
        if disc > 0.0:
            t = -b - math.sqrt(disc)
            if 0.001 < t < closest_t:
                closest_t = t
                hit = i
    return hit, closest_t

def run(w, h):
    spheres = [
        ((0.0, 0.0, -5.0), 1.0),
        ((1.5, 0.0, -6.0), 0.8),
        ((-1.5, 0.5, -7.0), 1.2),
    ]
    hits = 0
    for y in range(h):
        for x in range(w):
            dx = (x / w) - 0.5
            dy = (y / h) - 0.5
            d = (dx, dy, -1.0)
            inv = 1.0 / math.sqrt(dot(d, d))
            d = (d[0]*inv, d[1]*inv, d[2]*inv)
            h_, _ = trace((0.0, 0.0, 0.0), d, spheres)
            if h_ >= 0:
                hits += 1
    return hits

run(max(8, 80 // _S), max(8, 60 // _S))
