// gen-tray-icon.swift — the menu bar icons: a cat's head that tires as the
// day goes on, and sleeps through a break.
//
// MyGo's Tray takes PNG bytes, so the heads are drawn once here rather than
// at runtime. Each mood is one file in icon/; rerun only when a shape or the
// size changes:
//
//   swift scripts/gen-tray-icon.swift icon/tray.png 32 awake
//   swift scripts/gen-tray-icon.swift icon/tray-tired.png 32 tired
//   swift scripts/gen-tray-icon.swift icon/tray-weary.png 32 weary
//   swift scripts/gen-tray-icon.swift icon/tray-exhausted.png 32 exhausted
//   swift scripts/gen-tray-icon.swift icon/tray-break.png 32 asleep
//
// The moods: awake has round open eyes; tired has the lids half down;
// weary has them almost shut and the ears tilting out; exhausted has shut
// eyes slanting down, flat ears, a lower head and a drop of sweat; asleep
// has closed lids curving down, content.
//
// 32 pixels wide is 16 points at 2x, which is what a menu bar icon is. The
// head is pure black on transparent, with the eyes and nose cut out:
// IconIsTemplate makes macOS tint it. A larger pixel count draws the same
// shape bigger, to look at.

import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let args = CommandLine.arguments
let out = args.count > 1 ? args[1] : "icon/tray.png"
let px = args.count > 2 ? Int(args[2])! : 32
let mood = args.count > 3 ? args[3] : "awake"
guard ["awake", "tired", "weary", "exhausted", "asleep"].contains(mood) else {
    FileHandle.standardError.write("unknown mood \(mood)\n".data(using: .utf8)!)
    exit(2)
}

guard let ctx = CGContext(data: nil, width: px, height: px, bitsPerComponent: 8, bytesPerRow: 0,
                          space: CGColorSpaceCreateDeviceRGB(),
                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { exit(1) }
// Draw on a 32-unit grid, y down.
let unit = CGFloat(px) / 32
ctx.translateBy(x: 0, y: CGFloat(px))
ctx.scaleBy(x: unit, y: -unit)

// The left ear's tip; the right one mirrors it. A tired cat's ears tilt out
// and down, and an exhausted cat's head sinks a little.
let earTip: CGPoint
var sink: CGFloat = 0
switch mood {
case "weary": earTip = CGPoint(x: 4.2, y: 3.4)
case "exhausted": earTip = CGPoint(x: 2.6, y: 5.4); sink = 1
default: earTip = CGPoint(x: 5.5, y: 2.5)
}
ctx.translateBy(x: 0, y: sink)

// The head: two pointed ears, a gently arched top between them, straight
// cheeks and a rounded chin. The inner edge of each ear bows out a little.
func mirror(_ p: CGPoint) -> CGPoint { CGPoint(x: 32 - p.x, y: p.y) }
let earBase = CGPoint(x: 12.5, y: 9)
let earBow = CGPoint(x: (earTip.x + earBase.x) / 2 + 0.5, y: (earTip.y + earBase.y) / 2 - 0.75)
let head = CGMutablePath()
head.move(to: CGPoint(x: 2.5, y: 22))
head.addLine(to: CGPoint(x: 2.5, y: 14))
head.addLine(to: earTip)
head.addQuadCurve(to: earBase, control: earBow)
head.addQuadCurve(to: mirror(earBase), control: CGPoint(x: 16, y: 7.8))
head.addQuadCurve(to: mirror(earTip), control: mirror(earBow))
head.addLine(to: CGPoint(x: 29.5, y: 14))
head.addLine(to: CGPoint(x: 29.5, y: 22))
head.addQuadCurve(to: CGPoint(x: 21.5, y: 30), control: CGPoint(x: 29.5, y: 30))
head.addLine(to: CGPoint(x: 10.5, y: 30))
head.addQuadCurve(to: CGPoint(x: 2.5, y: 22), control: CGPoint(x: 2.5, y: 30))
head.closeSubpath()
ctx.setFillColor(CGColor(red: 0, green: 0, blue: 0, alpha: 1))
ctx.addPath(head)
ctx.fillPath()

// Eyes and nose are cut out of the head, and an exhausted cat's drop of
// sweat on its right temple.
ctx.setBlendMode(.clear)
if mood == "exhausted" {
    let drop = CGMutablePath()
    drop.move(to: CGPoint(x: 25.6, y: 10.6))
    drop.addQuadCurve(to: CGPoint(x: 27.0, y: 14.4), control: CGPoint(x: 27.0, y: 12.6))
    drop.addArc(center: CGPoint(x: 25.6, y: 14.4), radius: 1.4, startAngle: 0, endAngle: .pi, clockwise: false)
    drop.addQuadCurve(to: CGPoint(x: 25.6, y: 10.6), control: CGPoint(x: 24.2, y: 12.6))
    drop.closeSubpath()
    ctx.addPath(drop)
    ctx.fillPath()
}
ctx.setLineCap(.round)
for (x, lean) in [(11.0, 0.12), (21.0, -0.12)] {
    // outward is -1 for the left eye and 1 for the right.
    let outward: CGFloat = x < 16 ? -1 : 1
    switch mood {
    case "asleep":
        // Closed lids curving down.
        let lid = CGMutablePath()
        lid.move(to: CGPoint(x: x - 2.6, y: 18.4))
        lid.addQuadCurve(to: CGPoint(x: x + 2.6, y: 18.4), control: CGPoint(x: x, y: 22.4))
        ctx.addPath(lid)
        ctx.setLineWidth(2.2)
        ctx.strokePath()
    case "exhausted":
        // Shut, slanting down to the outside.
        ctx.move(to: CGPoint(x: x - 2.4 * outward, y: 18.6))
        ctx.addLine(to: CGPoint(x: x + 2.4 * outward, y: 20.4))
        ctx.setLineWidth(2.2)
        ctx.strokePath()
    default:
        // An upright oval leaning a little inward; a tired lid hides its
        // top, flat across.
        let lidLine: CGFloat = ["tired": 18.6, "weary": 20.0][mood] ?? 0
        ctx.saveGState()
        if lidLine > 0 {
            ctx.clip(to: CGRect(x: x - 4, y: lidLine, width: 8, height: 8))
        }
        ctx.translateBy(x: x, y: 19)
        ctx.rotate(by: lean)
        let width: CGFloat = mood == "weary" ? 4.4 : 3.4
        ctx.fillEllipse(in: CGRect(x: -width / 2, y: -3.2, width: width, height: 6.4))
        ctx.restoreGState()
    }
}
ctx.fillEllipse(in: CGRect(x: 14.8, y: 23.6, width: 2.4, height: 1.6))

guard let image = ctx.makeImage(),
      let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: out) as CFURL,
                                                 UTType.png.identifier as CFString, 1, nil) else { exit(1) }
CGImageDestinationAddImage(dest, image, nil)
guard CGImageDestinationFinalize(dest) else { exit(1) }
print("wrote \(out): \(px)x\(px) pixels (\(mood))")
