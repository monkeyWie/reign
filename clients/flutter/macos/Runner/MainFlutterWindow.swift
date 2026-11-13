import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow, NSWindowDelegate {
  override func awakeFromNib() {
    self.minSize = NSSize(width: 720, height: 540)
    let flutterViewController = FlutterViewController()
    let windowFrame = self.frame
    self.contentViewController = flutterViewController
    self.setFrame(windowFrame, display: true)
    if let screen = NSScreen.main {
      self.setContentSize(NSSize(width: min(1200, screen.visibleFrame.width - 80), height: min(780, screen.visibleFrame.height - 80)))
      self.center()
    }

    RegisterGeneratedPlugins(registry: flutterViewController)

    super.awakeFromNib()
    self.title = "Agent Hub"
    self.delegate = self
  }

  func windowShouldClose(_ sender: NSWindow) -> Bool {
    // Hiding the window keeps its Flutter engine and embedded Go service alive.
    sender.orderOut(nil)
    return false
  }
}
