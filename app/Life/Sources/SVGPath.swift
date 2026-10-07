import SwiftUI

// A source's logo is one SVG path in a 24×24 box (hub brand/logos.go, Simple
// Icons). SwiftUI has no SVG reader, so this turns the path's `d` string into a
// Path: M L H V C S Q T A Z, relative and absolute, implicit repeats, compact
// numbers ("1.5.5", "-.5"), arcs as béziers. Fill is nonzero, like the
// browser's default for a plain <path>.

/// The logo drawn to fit its frame, 24×24 scaled uniformly and centred.
struct SVGPathShape: Shape {
    let d: String
    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        let t = CGAffineTransform(translationX: rect.midX - 12 * s, y: rect.midY - 12 * s).scaledBy(x: s, y: s)
        return SVGPath.parse(d).applying(t)
    }
}

enum SVGPath {
    // Guarded by `lock`.
    nonisolated(unsafe) private static var cache: [String: Path] = [:]
    private static let lock = NSLock()

    static func parse(_ d: String) -> Path {
        lock.lock(); defer { lock.unlock() }
        if let p = cache[d] { return p }
        let p = build(d)
        cache[d] = p
        return p
    }

    private enum Tok { case cmd(Character), num(String) }

    private static func tokens(_ d: String) -> [Tok] {
        var out: [Tok] = []
        let cs = Array(d.unicodeScalars)
        var i = 0
        while i < cs.count {
            let c = cs[i]
            if CharacterSet.letters.contains(c), c != "e", c != "E" {
                out.append(.cmd(Character(c))); i += 1; continue
            }
            if c == "-" || c == "+" || c == "." || ("0"..."9").contains(c) {
                var j = i
                if cs[j] == "-" || cs[j] == "+" { j += 1 }
                var dot = false, exp = false
                while j < cs.count {
                    let x = cs[j]
                    if ("0"..."9").contains(x) { j += 1 }
                    else if x == "." && !dot && !exp { dot = true; j += 1 }
                    else if (x == "e" || x == "E") && !exp {
                        exp = true; j += 1
                        if j < cs.count, cs[j] == "-" || cs[j] == "+" { j += 1 }
                    } else { break }
                }
                out.append(.num(String(String.UnicodeScalarView(cs[i..<j]))))
                i = j; continue
            }
            i += 1 // separators: space, comma
        }
        return out
    }

    private static func build(_ d: String) -> Path {
        var p = Path()
        var toks = tokens(d)
        var i = 0
        var cur = CGPoint.zero, start = CGPoint.zero
        var lastCtl: CGPoint? = nil      // the previous C/S second control, for S
        var lastQ: CGPoint? = nil        // the previous Q/T control, for T
        var cmd: Character = "M"

        func num() -> CGFloat? {
            guard i < toks.count, case .num(let s) = toks[i] else { return nil }
            i += 1; return CGFloat(Double(s) ?? 0)
        }
        // Arc flags are one character each and svgo packs them with what
        // follows ("a1 1 0 011 1" = flags 0, 1 then x 1): take the flag's one
        // digit and leave the rest of the token in place.
        func flag() -> CGFloat? {
            guard i < toks.count, case .num(let s) = toks[i], let f = s.first, f == "0" || f == "1" else { return nil }
            let rest = String(s.dropFirst())
            if rest.isEmpty { i += 1 } else { toks[i] = .num(rest) }
            return f == "1" ? 1 : 0
        }

        while i < toks.count {
            if case .cmd(let c) = toks[i] { cmd = c; i += 1 }
            let rel = cmd.isLowercase
            let o = rel ? cur : .zero
            switch cmd.uppercased().first! {
            case "M":
                guard let x = num(), let y = num() else { i += 1; continue }
                cur = CGPoint(x: o.x + x, y: o.y + y); start = cur
                p.move(to: cur)
                cmd = rel ? "l" : "L"   // further pairs are lines
                lastCtl = nil; lastQ = nil
            case "L":
                guard let x = num(), let y = num() else { i += 1; continue }
                cur = CGPoint(x: o.x + x, y: o.y + y); p.addLine(to: cur)
                lastCtl = nil; lastQ = nil
            case "H":
                guard let x = num() else { i += 1; continue }
                cur = CGPoint(x: (rel ? cur.x : 0) + x, y: cur.y); p.addLine(to: cur)
                lastCtl = nil; lastQ = nil
            case "V":
                guard let y = num() else { i += 1; continue }
                cur = CGPoint(x: cur.x, y: (rel ? cur.y : 0) + y); p.addLine(to: cur)
                lastCtl = nil; lastQ = nil
            case "C":
                guard let a = num(), let b = num(), let c = num(), let e = num(), let x = num(), let y = num() else { i += 1; continue }
                let c1 = CGPoint(x: o.x + a, y: o.y + b), c2 = CGPoint(x: o.x + c, y: o.y + e)
                cur = CGPoint(x: o.x + x, y: o.y + y)
                p.addCurve(to: cur, control1: c1, control2: c2)
                lastCtl = c2; lastQ = nil
            case "S":
                guard let c = num(), let e = num(), let x = num(), let y = num() else { i += 1; continue }
                let c1 = lastCtl.map { CGPoint(x: 2 * cur.x - $0.x, y: 2 * cur.y - $0.y) } ?? cur
                let c2 = CGPoint(x: o.x + c, y: o.y + e)
                cur = CGPoint(x: o.x + x, y: o.y + y)
                p.addCurve(to: cur, control1: c1, control2: c2)
                lastCtl = c2; lastQ = nil
            case "Q":
                guard let a = num(), let b = num(), let x = num(), let y = num() else { i += 1; continue }
                let q = CGPoint(x: o.x + a, y: o.y + b)
                cur = CGPoint(x: o.x + x, y: o.y + y)
                p.addQuadCurve(to: cur, control: q)
                lastQ = q; lastCtl = nil
            case "T":
                guard let x = num(), let y = num() else { i += 1; continue }
                let q = lastQ.map { CGPoint(x: 2 * cur.x - $0.x, y: 2 * cur.y - $0.y) } ?? cur
                cur = CGPoint(x: o.x + x, y: o.y + y)
                p.addQuadCurve(to: cur, control: q)
                lastQ = q; lastCtl = nil
            case "A":
                guard let rx = num(), let ry = num(), let rot = num(),
                      let large = flag(), let sweep = flag(), let x = num(), let y = num() else { i += 1; continue }
                let to = CGPoint(x: o.x + x, y: o.y + y)
                arc(&p, from: cur, to: to, rx: rx, ry: ry, rot: rot, large: large != 0, sweep: sweep != 0)
                cur = to
                lastCtl = nil; lastQ = nil
            case "Z":
                p.closeSubpath(); cur = start
                lastCtl = nil; lastQ = nil
                // a number straight after Z has no command; skip it
                if i < toks.count, case .num = toks[i] { i += 1 }
            default:
                i += 1
            }
        }
        return p
    }

    /// SVG's endpoint arc (spec F.6.5) as up-to-90° bézier segments.
    private static func arc(_ p: inout Path, from p1: CGPoint, to p2: CGPoint, rx rx0: CGFloat, ry ry0: CGFloat,
                            rot: CGFloat, large: Bool, sweep: Bool) {
        var rx = abs(rx0), ry = abs(ry0)
        if rx == 0 || ry == 0 || p1 == p2 { p.addLine(to: p2); return }
        let phi = rot * .pi / 180, cp = cos(phi), sp = sin(phi)
        let dx = (p1.x - p2.x) / 2, dy = (p1.y - p2.y) / 2
        let x1 = cp * dx + sp * dy, y1 = -sp * dx + cp * dy
        let lam = (x1 * x1) / (rx * rx) + (y1 * y1) / (ry * ry)
        if lam > 1 { rx *= sqrt(lam); ry *= sqrt(lam) }
        let num = rx * rx * ry * ry - rx * rx * y1 * y1 - ry * ry * x1 * x1
        let den = rx * rx * y1 * y1 + ry * ry * x1 * x1
        var co = sqrt(max(0, num / den))
        if large == sweep { co = -co }
        let cx1 = co * rx * y1 / ry, cy1 = -co * ry * x1 / rx
        let cx = cp * cx1 - sp * cy1 + (p1.x + p2.x) / 2
        let cy = sp * cx1 + cp * cy1 + (p1.y + p2.y) / 2
        func ang(_ ux: CGFloat, _ uy: CGFloat, _ vx: CGFloat, _ vy: CGFloat) -> CGFloat {
            let a = atan2(ux * vy - uy * vx, ux * vx + uy * vy)
            return a
        }
        let t1 = ang(1, 0, (x1 - cx1) / rx, (y1 - cy1) / ry)
        var dt = ang((x1 - cx1) / rx, (y1 - cy1) / ry, (-x1 - cx1) / rx, (-y1 - cy1) / ry)
        if !sweep && dt > 0 { dt -= 2 * .pi } else if sweep && dt < 0 { dt += 2 * .pi }
        let n = Int(ceil(abs(dt) / (.pi / 2)))
        let step = dt / CGFloat(n)
        let k = 4 / 3 * tan(step / 4)
        var a = t1
        func pt(_ t: CGFloat) -> (CGPoint, CGPoint) {
            let ex = rx * cos(t), ey = ry * sin(t)
            let ddx = -rx * sin(t), ddy = ry * cos(t)
            return (CGPoint(x: cx + cp * ex - sp * ey, y: cy + sp * ex + cp * ey),
                    CGPoint(x: cp * ddx - sp * ddy, y: sp * ddx + cp * ddy))
        }
        for s in 0..<n {
            let (a0, d0) = pt(a), b = a + step
            let (b0, d1) = pt(b)
            let end = s == n - 1 ? p2 : b0
            p.addCurve(to: end, control1: CGPoint(x: a0.x + k * d0.x, y: a0.y + k * d0.y),
                       control2: CGPoint(x: b0.x - k * d1.x, y: b0.y - k * d1.y))
            a = b
        }
    }
}
