import AppKit
import Foundation

let outDir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "app/AppIcon.iconset"
try? FileManager.default.createDirectory(atPath: outDir, withIntermediateDirectories: true)

func drawIcon(size: Int) -> NSBitmapImageRep {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size,
                               bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                               colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    rep.size = NSSize(width: size, height: size)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    let rect = NSRect(x: 0, y: 0, width: size, height: size)
    NSColor.clear.setFill()
    rect.fill()

    // Apple's macOS app-icon grid leaves roughly 10% breathing room around the
    // 824pt rounded square. Without this the icon looks oversized in the Dock.
    let inset = CGFloat(size) * 0.098
    let body = rect.insetBy(dx: inset, dy: inset)
    let radius = body.width * 0.225
    let path = NSBezierPath(roundedRect: body, xRadius: radius, yRadius: radius)
    path.addClip()
    let gradient = NSGradient(colors: [
        NSColor(calibratedRed: 0.46, green: 0.39, blue: 1.0, alpha: 1),
        NSColor(calibratedRed: 0.23, green: 0.16, blue: 0.72, alpha: 1),
        NSColor(calibratedRed: 0.08, green: 0.07, blue: 0.22, alpha: 1),
    ])!
    gradient.draw(in: body, angle: -55)

    // soft glow
    let glow = NSBezierPath(ovalIn: NSRect(x: body.minX + body.width * 0.05, y: body.maxY - body.height * 0.55,
                                           width: body.width * 0.75, height: body.height * 0.55))
    NSColor(calibratedWhite: 1, alpha: 0.10).setFill()
    glow.fill()

    let s = CGFloat(size)
    let center = NSPoint(x: s / 2, y: s / 2)
    let nodeColor = NSColor(calibratedWhite: 1, alpha: 0.92)
    let lineColor = NSColor(calibratedWhite: 1, alpha: 0.42)
    let nodes = [
        NSPoint(x: center.x, y: center.y + s * 0.19),
        NSPoint(x: center.x - s * 0.19, y: center.y - s * 0.10),
        NSPoint(x: center.x + s * 0.19, y: center.y - s * 0.10),
    ]
    let lw = max(2, s * 0.022)
    lineColor.setStroke()
    for i in 0..<nodes.count {
        let line = NSBezierPath()
        line.lineWidth = lw
        line.lineCapStyle = .round
        line.move(to: nodes[i])
        line.line(to: nodes[(i + 1) % nodes.count])
        line.stroke()
    }
    for n in nodes {
        let r = s * 0.072
        let circle = NSBezierPath(ovalIn: NSRect(x: n.x - r, y: n.y - r, width: r * 2, height: r * 2))
        nodeColor.setFill()
        circle.fill()
        let inner = NSBezierPath(ovalIn: NSRect(x: n.x - r * 0.38, y: n.y - r * 0.38, width: r * 0.76, height: r * 0.76))
        NSColor(calibratedRed: 0.23, green: 0.16, blue: 0.72, alpha: 0.9).setFill()
        inner.fill()
    }
    // sparkle at the top right
    let sx = center.x + s * 0.22
    let sy = center.y + s * 0.23
    let sparkle = NSBezierPath()
    sparkle.move(to: NSPoint(x: sx, y: sy + s * 0.045))
    sparkle.line(to: NSPoint(x: sx + s * 0.012, y: sy + s * 0.012))
    sparkle.line(to: NSPoint(x: sx + s * 0.045, y: sy))
    sparkle.line(to: NSPoint(x: sx + s * 0.012, y: sy - s * 0.012))
    sparkle.line(to: NSPoint(x: sx, y: sy - s * 0.045))
    sparkle.line(to: NSPoint(x: sx - s * 0.012, y: sy - s * 0.012))
    sparkle.line(to: NSPoint(x: sx - s * 0.045, y: sy))
    sparkle.line(to: NSPoint(x: sx - s * 0.012, y: sy + s * 0.012))
    sparkle.close()
    NSColor(calibratedWhite: 1, alpha: 0.95).setFill()
    sparkle.fill()

    NSGraphicsContext.restoreGraphicsState()
    return rep
}

let entries: [(Int, String)] = [
    (16, "icon_16x16.png"), (32, "icon_16x16@2x.png"),
    (32, "icon_32x32.png"), (64, "icon_32x32@2x.png"),
    (128, "icon_128x128.png"), (256, "icon_128x128@2x.png"),
    (256, "icon_256x256.png"), (512, "icon_256x256@2x.png"),
    (512, "icon_512x512.png"), (1024, "icon_512x512@2x.png"),
]
for (size, name) in entries {
    let rep = drawIcon(size: size)
    let data = rep.representation(using: .png, properties: [:])!
    try data.write(to: URL(fileURLWithPath: outDir).appendingPathComponent(name))
}
print("wrote \(outDir)")
