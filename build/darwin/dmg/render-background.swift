#!/usr/bin/env swift
import AppKit
import Foundation

let pointWidth = 660
let pointHeight = 360
let outputDirectory = CommandLine.arguments.count == 2
    ? URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
    : nil
guard let outputDirectory else {
    fputs("usage: render-background.swift OUTPUT_DIR\n", stderr)
    exit(64)
}

func color(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat) -> NSColor {
    NSColor(calibratedRed: red / 255, green: green / 255, blue: blue / 255, alpha: 1)
}

func drawText(_ text: String, y: CGFloat, size: CGFloat, weight: NSFont.Weight,
              color textColor: NSColor) {
    let paragraph = NSMutableParagraphStyle()
    paragraph.alignment = .center
    let attributes: [NSAttributedString.Key: Any] = [
        .font: NSFont.systemFont(ofSize: size, weight: weight),
        .foregroundColor: textColor,
        .paragraphStyle: paragraph,
    ]
    NSString(string: text).draw(
        in: NSRect(x: 40, y: y, width: 580, height: size + 12),
        withAttributes: attributes
    )
}

func render(scale: Int, filename: String) throws {
    guard let bitmap = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: pointWidth * scale,
        pixelsHigh: pointHeight * scale,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ) else { throw NSError(domain: "LoquiDMG", code: 1) }
    guard let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
        throw NSError(domain: "LoquiDMG", code: 3)
    }
    let previousContext = NSGraphicsContext.current
    NSGraphicsContext.current = context
    defer { NSGraphicsContext.current = previousContext }
    context.cgContext.scaleBy(x: CGFloat(scale), y: CGFloat(scale))
    let bounds = NSRect(x: 0, y: 0, width: pointWidth, height: pointHeight)
    NSGradient(
        starting: color(251, 250, 255),
        ending: color(241, 241, 255)
    )!.draw(in: bounds, angle: -90)

    drawText("Drag Loqui to Applications", y: 298, size: 24, weight: .semibold,
             color: color(31, 31, 46))
    drawText("Arrastra Loqui a Aplicaciones", y: 267, size: 17, weight: .medium,
             color: color(92, 92, 121))

    let arrowColor = color(91, 92, 246)
    arrowColor.setFill()
    let arrow = NSBezierPath()
    arrow.move(to: NSPoint(x: 270, y: 139))
    arrow.line(to: NSPoint(x: 357, y: 139))
    arrow.line(to: NSPoint(x: 357, y: 124))
    arrow.line(to: NSPoint(x: 397, y: 145))
    arrow.line(to: NSPoint(x: 357, y: 166))
    arrow.line(to: NSPoint(x: 357, y: 151))
    arrow.line(to: NSPoint(x: 270, y: 151))
    arrow.close()
    arrow.fill()

    guard let data = bitmap.representation(using: .png, properties: [:]) else {
        throw NSError(domain: "LoquiDMG", code: 2)
    }
    try data.write(to: outputDirectory.appendingPathComponent(filename), options: .atomic)
}

try FileManager.default.createDirectory(
    at: outputDirectory,
    withIntermediateDirectories: true
)
try render(scale: 1, filename: "background.png")
try render(scale: 2, filename: "background@2x.png")
