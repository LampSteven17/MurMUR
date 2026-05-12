// Package tui — 3D rotating skull renderer.
//
// Pipeline (per frame):
//   1. Raymarch each pixel against the skull SDF, recording brightness AND
//      depth into per-pixel buffers.
//   2. Post-pass: silhouette + internal edge detection on the depth buffer
//      (Sobel-style). Edge cells override the density char with `#` so the
//      outline reads crisply against the shaded body.
//   3. Emit ASCII.
//
// Camera/aspect follow the vgel.me convention: camera at z = -K outside the
// bounding sphere, screen [-1,1]² mapped to ray dirs (u*sx, v*sy, 1) where
// sy = sx * 2 * rows/cols compensates for terminal cells being ~2× taller
// than wide.
//
// The SDF composes a cranium ellipsoid + jaw ellipsoid (hard-min so the seam
// reads as a jawline) with carve-outs for eye sockets, nasal cavity, and a
// mouth slit with discrete tooth gaps.
package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type vec3 struct{ x, y, z float64 }

func (a vec3) sub(b vec3) vec3    { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a vec3) length() float64    { return math.Sqrt(a.x*a.x + a.y*a.y + a.z*a.z) }
func (a vec3) dot(b vec3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }

func (a vec3) normalize() vec3 {
	l := a.length()
	if l == 0 {
		return vec3{}
	}
	return vec3{a.x / l, a.y / l, a.z / l}
}

func (a vec3) rotateY(angle float64) vec3 {
	c, s := math.Cos(angle), math.Sin(angle)
	return vec3{c*a.x + s*a.z, a.y, -s*a.x + c*a.z}
}

// --- SDF primitives ---

func sdSphere(p vec3, r float64) float64 { return p.length() - r }

// sdCylinderZ is an infinite cylinder along the Z axis. Used for piercing
// voids that go right through the skull — clean black holes from the camera.
func sdCylinderZ(p vec3, r float64) float64 {
	return math.Sqrt(p.x*p.x+p.y*p.y) - r
}

// sdCapsule is the SDF for a cylinder-with-rounded-ends between points a and
// b, radius r. Used for the crossbones beneath the skull.
func sdCapsule(p, a, b vec3, r float64) float64 {
	pa := p.sub(a)
	ba := b.sub(a)
	dot := pa.dot(ba)
	baLen2 := ba.dot(ba)
	if baLen2 == 0 {
		return pa.length() - r
	}
	h := math.Max(0, math.Min(1, dot/baLen2))
	closest := vec3{a.x + ba.x*h, a.y + ba.y*h, a.z + ba.z*h}
	return p.sub(closest).length() - r
}

func sdEllipsoid(p, r vec3) float64 {
	if r.x == 0 || r.y == 0 || r.z == 0 {
		return 0
	}
	k := vec3{p.x / r.x, p.y / r.y, p.z / r.z}.length()
	return (k - 1.0) * math.Min(math.Min(r.x, r.y), r.z)
}

func opSubtract(a, b float64) float64 { return math.Max(a, -b) }

// smin is a polynomial smooth-min — softens the seam between two SDFs so the
// blend reads as a single organic body rather than two intersecting volumes.
func smin(a, b, k float64) float64 {
	h := math.Max(k-math.Abs(a-b), 0) / k
	return math.Min(a, b) - h*h*k*0.25
}

// --- Skull composite SDF ---

// sdSkull is in skull-local coordinates. Camera-facing side is at z < 0.
//
// Anatomy:
//   cranium — rounded dome at top
//   cheekbones — slight flares at temple level for the characteristic skull silhouette
//   mandible — narrow, pointed jaw
//   eye sockets — large Z-axis cylinders piercing the cranium (clean black voids)
//   nose — vertical triangular ellipsoid between eyes
//   mouth — wide thin slit at the upper jaw
//   tooth gaps — 7 small vertical carves forming the upper-teeth seam
func sdSkull(p vec3) float64 {
	// Cranium — slightly squashed, wider than tall for a more skull-like dome.
	cranium := sdEllipsoid(p.sub(vec3{0, 0.15, 0}), vec3{0.88, 0.78, 0.85})

	// Cheekbones — flaring out at temple/cheek level.
	leftCheek := sdEllipsoid(p.sub(vec3{-0.45, -0.10, -0.28}), vec3{0.20, 0.22, 0.30})
	rightCheek := sdEllipsoid(p.sub(vec3{0.45, -0.10, -0.28}), vec3{0.20, 0.22, 0.30})

	// Mandible — TWO stacked ellipsoids forming a tapered jaw. Upper is
	// wide where it joins the cheeks; lower is narrower, forming the chin
	// point. Smin'd together so they read as one organic jaw.
	jawUpper := sdEllipsoid(p.sub(vec3{0, -0.38, -0.05}), vec3{0.42, 0.18, 0.46})
	jawLower := sdEllipsoid(p.sub(vec3{0, -0.68, -0.05}), vec3{0.26, 0.22, 0.36})
	jaw := smin(jawUpper, jawLower, 0.15)

	// Body — cranium + cheeks hard-min'd (crisp jawline reveal), jaw smin'd
	// in for an organic chin curve.
	body := math.Min(cranium, math.Min(leftCheek, rightCheek))
	body = smin(body, jaw, 0.10)

	// Eye sockets — big Z-axis cylinder voids piercing the cranium.
	leftEye := sdCylinderZ(p.sub(vec3{-0.30, 0.22, 0}), 0.17)
	rightEye := sdCylinderZ(p.sub(vec3{0.30, 0.22, 0}), 0.17)
	body = opSubtract(body, leftEye)
	body = opSubtract(body, rightEye)

	// Nose hole — vertical thin ellipsoid piercing through.
	nose := sdEllipsoid(p.sub(vec3{0, -0.05, 0}), vec3{0.06, 0.14, 1.0})
	body = opSubtract(body, nose)

	// Mouth slit — narrower than cheekbone width so the cranium+jaw stay
	// connected through the cheek bridge above the slit.
	mouth := sdEllipsoid(p.sub(vec3{0, -0.36, 0}), vec3{0.22, 0.045, 1.0})
	body = opSubtract(body, mouth)

	// Tooth gaps — 5 small vertical slots above the mouth (upper teeth seam).
	for i := -2; i <= 2; i++ {
		gx := float64(i) * 0.08
		gap := sdEllipsoid(p.sub(vec3{gx, -0.30, -0.40}), vec3{0.010, 0.05, 0.20})
		body = opSubtract(body, gap)
	}

	// Crossbones — two capsules forming an X just below the chin. The
	// bones rotate with the skull (they live in skull-local coords).
	const boneR = 0.07
	const capR = 0.13
	bone1 := sdCapsule(p, vec3{-0.75, -1.05, 0}, vec3{0.75, -1.50, 0}, boneR)
	bone2 := sdCapsule(p, vec3{0.75, -1.05, 0}, vec3{-0.75, -1.50, 0}, boneR)
	cap1L := sdSphere(p.sub(vec3{-0.75, -1.05, 0}), capR)
	cap1R := sdSphere(p.sub(vec3{0.75, -1.50, 0}), capR)
	cap2L := sdSphere(p.sub(vec3{0.75, -1.05, 0}), capR)
	cap2R := sdSphere(p.sub(vec3{-0.75, -1.50, 0}), capR)
	bones := math.Min(bone1, bone2)
	bones = math.Min(bones, math.Min(math.Min(cap1L, cap1R), math.Min(cap2L, cap2R)))
	body = math.Min(body, bones)

	return body
}

func sdNormal(p vec3) vec3 {
	const eps = 0.005
	dx := sdSkull(vec3{p.x + eps, p.y, p.z}) - sdSkull(vec3{p.x - eps, p.y, p.z})
	dy := sdSkull(vec3{p.x, p.y + eps, p.z}) - sdSkull(vec3{p.x, p.y - eps, p.z})
	dz := sdSkull(vec3{p.x, p.y, p.z + eps}) - sdSkull(vec3{p.x, p.y, p.z - eps})
	return vec3{dx, dy, dz}.normalize()
}

// Donut.c-style 12-char density gradient, dim → bright.
var skullDensity = []byte(".,-~:;=!*#$@")

// Skull3DFrame raymarches a rotated skull, runs edge detection on the depth
// buffer, and returns the rasterized ASCII frame (no styling applied).
func Skull3DFrame(yaw float64, cols, rows int) string {
	if cols < 4 || rows < 4 {
		return ""
	}

	// Composition is taller (skull + crossbones); camera distance and FOV
	// tuned so the full emblem fits in a 60×28 frame.
	const cameraZ = -5.5
	const sx = 0.32
	sy := sx * 2.0 * float64(rows) / float64(cols)

	// Light mostly from camera direction so the entire front face reads
	// as a bright canvas. A small +y component prevents the chin from
	// going darker than the forehead, which otherwise creates a "two-blob"
	// illusion. Small +x adds a subtle right-side highlight.
	light := vec3{0.20, 0.30, -0.93}.normalize()

	// Per-pixel buffers.
	const noHit = math.MaxFloat64
	chars := make([][]byte, rows)
	depths := make([][]float64, rows)
	for y := range chars {
		chars[y] = make([]byte, cols)
		depths[y] = make([]float64, cols)
		for x := range chars[y] {
			chars[y][x] = ' '
			depths[y][x] = noHit
		}
	}

	// --- Raymarch pass ---
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			u := (float64(x)/float64(cols-1))*2.0 - 1.0
			v := 1.0 - (float64(y)/float64(rows-1))*2.0
			dir := vec3{u * sx, v * sy, 1.0}.normalize()
			origin := vec3{0, 0, cameraZ}

			t := 0.0
			hit := false
			for step := 0; step < 128; step++ {
				pt := vec3{
					origin.x + dir.x*t,
					origin.y + dir.y*t,
					origin.z + dir.z*t,
				}
				rotated := pt.rotateY(-yaw)
				d := sdSkull(rotated)
				if d < 0.001 {
					hit = true
					break
				}
				t += d
				if t > 20.0 {
					break
				}
			}
			if !hit {
				continue
			}

			pt := vec3{
				origin.x + dir.x*t,
				origin.y + dir.y*t,
				origin.z + dir.z*t,
			}
			rotated := pt.rotateY(-yaw)
			normal := sdNormal(rotated).rotateY(yaw)

			brightness := math.Max(0, normal.dot(light))
			brightness = 0.25 + 0.75*brightness

			idx := int(brightness * float64(len(skullDensity)-1))
			if idx < 0 {
				idx = 0
			}
			if idx > len(skullDensity)-1 {
				idx = len(skullDensity) - 1
			}
			chars[y][x] = skullDensity[idx]
			depths[y][x] = t
		}
	}

	// --- Edge-detection pass ---
	//
	// Two kinds of edges, both overridden with `#`:
	//   1. Silhouette: a hit pixel touching at least one non-hit neighbor.
	//   2. Internal: large depth discontinuity vs. orthogonal neighbors.
	const edgeThreshold = 0.35
	out := make([][]byte, rows)
	for y := range out {
		out[y] = make([]byte, cols)
		copy(out[y], chars[y])
	}
	for y := 1; y < rows-1; y++ {
		for x := 1; x < cols-1; x++ {
			if depths[y][x] == noHit {
				continue
			}
			// Silhouette
			if depths[y][x+1] == noHit || depths[y][x-1] == noHit ||
				depths[y+1][x] == noHit || depths[y-1][x] == noHit {
				out[y][x] = '#'
				continue
			}
			// Internal depth-discontinuity edge
			gx := math.Abs(depths[y][x+1] - depths[y][x-1])
			gy := math.Abs(depths[y+1][x] - depths[y-1][x])
			if gx+gy > edgeThreshold {
				out[y][x] = '#'
			}
		}
	}

	var sb strings.Builder
	sb.Grow(cols*rows + rows)
	for y := 0; y < rows; y++ {
		sb.Write(out[y])
		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// RenderSkull3D wraps the rasterized frame in the Athanor palette — ivory
// ink everywhere with vermilion (rubrication red) over-tints on the
// brightest highlights for a bloody-skull feel.
func RenderSkull3D(theme Theme, yaw float64, cols, rows int) string {
	frame := Skull3DFrame(yaw, cols, rows)

	ink := lipgloss.NewStyle().Foreground(theme.Ink)
	out := ink.Render(frame)

	highlight := lipgloss.NewStyle().Foreground(theme.Vermilion).Bold(true)
	for _, c := range []byte{'$', '@'} {
		s := string(c)
		out = strings.ReplaceAll(out, s, highlight.Render(s))
	}
	return out
}
