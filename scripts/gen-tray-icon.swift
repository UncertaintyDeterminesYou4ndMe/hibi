// gen-tray-icon.swift — the menu bar icon: a cat's head.
//
// MyGo's Tray takes PNG bytes, so the head is drawn once here rather than
// at runtime. The result lives in icon/tray.png; rerun only when the shape
// or the size changes:
//
//   swift scripts/gen-tray-icon.swift icon/tray.png [pixels]
//
// 32 pixels wide is 16 points at 2x, which is what a menu bar icon is. The
// head is pure black on transparent, with the eyes and nose cut out:
// IconIsTemplate makes macOS tint it. A larger pixel count draws the same
// shape bigger, to look at.

import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "icon/tray.png"
let px = CommandLine.arguments.count > 2 ? Int(CommandLine.arguments[2])! : 32

guard let ctx = CGContext(data: nil, width: px, height: px, bitsPerComponent: 8, bytesPerRow: 0,
                          space: CGColorSpaceCreateDeviceRGB(),
                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { exit(1) }
// Draw on a 32-unit grid, y down.
let unit = CGFloat(px) / 32
ctx.translateBy(x: 0, y: CGFloat(px))
ctx.scaleBy(x: unit, y: -unit)

// The head: two pointed ears, a gently arched top between them, straight
// cheeks and a rounded chin.
let head = CGMutablePath()
head.move(to: CGPoint(x: 2.5, y: 22))
head.addLine(to: CGPoint(x: 2.5, y: 14))
head.addLine(to: CGPoint(x: 5.5, y: 2.5))
head.addQuadCurve(to: CGPoint(x: 12.5, y: 9), control: CGPoint(x: 9.5, y: 5))
head.addQuadCurve(to: CGPoint(x: 19.5, y: 9), control: CGPoint(x: 16, y: 7.8))
head.addQuadCurve(to: CGPoint(x: 26.5, y: 2.5), control: CGPoint(x: 22.5, y: 5))
head.addLine(to: CGPoint(x: 29.5, y: 14))
head.addLine(to: CGPoint(x: 29.5, y: 22))
head.addQuadCurve(to: CGPoint(x: 21.5, y: 30), control: CGPoint(x: 29.5, y: 30))
head.addLine(to: CGPoint(x: 10.5, y: 30))
head.addQuadCurve(to: CGPoint(x: 2.5, y: 22), control: CGPoint(x: 2.5, y: 30))
head.closeSubpath()
ctx.setFillColor(CGColor(red: 0, green: 0, blue: 0, alpha: 1))
ctx.addPath(head)
ctx.fillPath()

// Eyes: upright ovals, leaning a little inward; the nose a small oval
// between and below them.
ctx.setBlendMode(.clear)
for (x, lean) in [(11.0, 0.12), (21.0, -0.12)] {
    ctx.saveGState()
    ctx.translateBy(x: x, y: 19)
    ctx.rotate(by: lean)
    ctx.fillEllipse(in: CGRect(x: -1.7, y: -3.2, width: 3.4, height: 6.4))
    ctx.restoreGState()
}
ctx.fillEllipse(in: CGRect(x: 14.8, y: 23.6, width: 2.4, height: 1.6))

guard let image = ctx.makeImage(),
      let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: out) as CFURL,
                                                 UTType.png.identifier as CFString, 1, nil) else { exit(1) }
CGImageDestinationAddImage(dest, image, nil)
guard CGImageDestinationFinalize(dest) else { exit(1) }
print("wrote \(out): \(px)x\(px) pixels")
