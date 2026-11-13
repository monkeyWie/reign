import Cocoa
import Darwin
import FlutterMacOS

@main
class AppDelegate: FlutterAppDelegate {
  private var instanceLock: Int32 = -1
  private var statusItem: NSStatusItem?
  private var terminating = false
  private let activateNotification = Notification.Name("dev.agenthub.agenthubClient.activate")

  override func applicationWillFinishLaunching(_ notification: Notification) {
    // The installer and Flutter runner share one lock, even when launched by
    // absolute path or with `open -n` from two different bundle locations.
    do {
      let support = try FileManager.default.url(
        for: .applicationSupportDirectory, in: .userDomainMask,
        appropriateFor: nil, create: true)
      let directory = support.appendingPathComponent("dev.agenthub.agenthubClient", isDirectory: true)
      try FileManager.default.createDirectory(
        at: directory, withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700])
      // Discovery subprocesses must not inherit a Documents/Desktop working
      // directory just because the app was opened from a development checkout.
      // Agent sessions pass their own explicit workspace to the provider.
      guard FileManager.default.changeCurrentDirectoryPath(directory.path) else {
        throw CocoaError(.fileReadUnknown)
      }
      instanceLock = Darwin.open(
        directory.appendingPathComponent("desktop.lock").path,
        O_CREAT | O_RDWR | O_CLOEXEC, mode_t(0o600))
      guard instanceLock >= 0 else { throw CocoaError(.fileWriteUnknown) }
      if flock(instanceLock, LOCK_EX | LOCK_NB) != 0 {
        guard errno == EWOULDBLOCK else { throw CocoaError(.fileWriteUnknown) }
        DistributedNotificationCenter.default().postNotificationName(
          activateNotification, object: nil, userInfo: nil, deliverImmediately: true)
        Darwin.close(instanceLock)
        exit(EXIT_SUCCESS)
      }
    } catch {
      NSLog("Agent Hub could not acquire its desktop instance lock: %@", error.localizedDescription)
      exit(EXIT_FAILURE)
    }
    DistributedNotificationCenter.default().addObserver(
      self, selector: #selector(activateExistingWindow),
      name: activateNotification, object: nil)
    super.applicationWillFinishLaunching(notification)
  }

  @objc private func activateExistingWindow() {
    NSApp.activate(ignoringOtherApps: true)
    for window in NSApp.windows where window is MainFlutterWindow {
      window.deminiaturize(nil)
      window.makeKeyAndOrderFront(nil)
    }
  }

  override func applicationDidFinishLaunching(_ notification: Notification) {
    super.applicationDidFinishLaunching(notification)
    let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    item.button?.image = NSImage(systemSymbolName: "network", accessibilityDescription: "Agent Hub")
    item.button?.toolTip = "Agent Hub"
    let menu = NSMenu()
    let show = NSMenuItem(title: "打开 Agent Hub", action: #selector(activateExistingWindow), keyEquivalent: "")
    show.target = self
    menu.addItem(show)
    menu.addItem(NSMenuItem.separator())
    let quit = NSMenuItem(title: "退出 Agent Hub", action: #selector(quitApplication), keyEquivalent: "q")
    quit.target = self
    menu.addItem(quit)
    item.menu = menu
    statusItem = item
  }

  @objc private func quitApplication() { NSApp.terminate(nil) }

  override func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
    if terminating { return .terminateLater }
    let library = Bundle.main.bundlePath + "/Contents/Frameworks/libagenthub.dylib"
    guard let handle = dlopen(library, RTLD_NOW | RTLD_NOLOAD) else { return .terminateNow }
    guard let symbol = dlsym(handle, "AgentHubShutdown") else {
      dlclose(handle)
      return .terminateNow
    }
    terminating = true
    let shutdown = unsafeBitCast(symbol, to: (@convention(c) () -> Void).self)
    // Keep the main thread responsive while Go cancels connections and flushes
    // SQLite. The Go runtime lives in this app, not in a detached executable.
    DispatchQueue.global(qos: .userInitiated).async {
      shutdown()
      DispatchQueue.main.async { sender.reply(toApplicationShouldTerminate: true) }
    }
    return .terminateLater
  }

  override func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
    activateExistingWindow()
    return true
  }

  override func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    return false
  }

  override func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool {
    return true
  }
}
