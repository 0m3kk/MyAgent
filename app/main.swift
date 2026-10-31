import Cocoa
import WebKit

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var core: Process?
    private var coreLog: FileHandle?
    private var serverURL: URL?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        // Set the Dock icon explicitly as well as via Info.plist. LaunchServices
        // can keep a generic icon cached for locally rebuilt ad-hoc bundles.
        if let iconURL = Bundle.main.url(forResource: "AppIcon", withExtension: "icns"),
           let icon = NSImage(contentsOf: iconURL) {
            NSApp.applicationIconImage = icon
        }
        buildMenu()
        do {
            let url = try startCore()
            serverURL = url
            buildWindow(url: url)
            NSApp.activate(ignoringOtherApps: true)
        } catch {
            showFatal(error)
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        // Closing the window is a full quit: stop the core and remove the app
        // from the Dock. Scheduled work requires the app to stay open.
        return true
    }

    func windowWillClose(_ notification: Notification) {
        // Closing the last window is a full quit, not a hide-to-Dock.
        NSApp.terminate(nil)
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { window?.makeKeyAndOrderFront(nil) }
        return true
    }

    func applicationWillTerminate(_ notification: Notification) {
        stopCore()
    }

    private func startCore() throws -> URL {
        let env = ProcessInfo.processInfo.environment
        let fm = FileManager.default
        let dataDir: URL
        if let path = env["MYAGENT_DATA_DIR"], !path.isEmpty {
            dataDir = URL(fileURLWithPath: path)
        } else {
            dataDir = fm.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Application Support/MyAgent")
        }
        try fm.createDirectory(at: dataDir, withIntermediateDirectories: true)

        guard let agentd = locate("agentd", envKey: "MYAGENT_AGENTD") else {
            throw NSError(domain: "MyAgent", code: 1, userInfo: [NSLocalizedDescriptionKey:
                "Cannot find the agentd core binary. Build the app with scripts/build-macos.sh or set MYAGENT_AGENTD."])
        }
        guard let ui = locate("ui", envKey: "MYAGENT_UI_DIR", isDirectory: true) else {
            throw NSError(domain: "MyAgent", code: 2, userInfo: [NSLocalizedDescriptionKey:
                "Cannot find the built UI. Run the UI build or set MYAGENT_UI_DIR."])
        }

        let logURL = dataDir.appendingPathComponent("agentd.log")
        if !fm.fileExists(atPath: logURL.path) {
            fm.createFile(atPath: logURL.path, contents: nil)
        }
        let log = try FileHandle(forWritingTo: logURL)
        log.seekToEndOfFile()
        coreLog = log

        let process = Process()
        process.executableURL = agentd
        process.arguments = ["-addr", "127.0.0.1:0", "-open=false", "-ui", ui.path,
                             "-parent-pid", String(ProcessInfo.processInfo.processIdentifier)]
        var childEnv = env
        childEnv["MYAGENT_DATA_DIR"] = dataDir.path
        if childEnv["MYAGENT_WORKSPACE"] == nil {
            childEnv["MYAGENT_WORKSPACE"] = fm.homeDirectoryForCurrentUser
                .appendingPathComponent("MyAgent Workspace").path
        }
        process.environment = childEnv
        process.standardOutput = log
        process.standardError = log
        try process.run()
        core = process

        let infoURL = dataDir.appendingPathComponent("api.json")
        let deadline = Date().addingTimeInterval(20)
        while Date() < deadline {
            if process.isRunning == false {
                throw NSError(domain: "MyAgent", code: 3, userInfo: [NSLocalizedDescriptionKey:
                    "The core exited during startup. See \(logURL.path)."])
            }
            if let data = try? Data(contentsOf: infoURL),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let urlString = obj["url"] as? String,
               let token = obj["token"] as? String,
               var components = URLComponents(string: urlString) {
                components.queryItems = [URLQueryItem(name: "token", value: token)]
                if let url = components.url { return url }
            }
            Thread.sleep(forTimeInterval: 0.15)
        }
        throw NSError(domain: "MyAgent", code: 4, userInfo: [NSLocalizedDescriptionKey:
            "The core did not become ready. See \(logURL.path)."])
    }

    private func locate(_ name: String, envKey: String, isDirectory: Bool = false) -> URL? {
        let fm = FileManager.default
        if let path = ProcessInfo.processInfo.environment[envKey], !path.isEmpty {
            var isDir: ObjCBool = false
            if fm.fileExists(atPath: path, isDirectory: &isDir) { return URL(fileURLWithPath: path) }
        }
        if let resources = Bundle.main.resourceURL {
            let candidate = resources.appendingPathComponent(name)
            if fm.fileExists(atPath: candidate.path) { return candidate }
        }
        let cwd = URL(fileURLWithPath: fm.currentDirectoryPath)
        let candidates = isDirectory
            ? [cwd.appendingPathComponent("ui/dist"), cwd.appendingPathComponent("dist/ui")]
            : [cwd.appendingPathComponent("bin/\(name)"), cwd.appendingPathComponent(name)]
        for candidate in candidates where fm.fileExists(atPath: candidate.path) {
            return candidate
        }
        return nil
    }

    private func buildWindow(url: URL) {
        let rect = NSRect(x: 0, y: 0, width: 1440, height: 940)
        window = NSWindow(contentRect: rect,
                          styleMask: [.titled, .closable, .miniaturizable, .resizable],
                          backing: .buffered, defer: false)
        window.title = "MyAgent"
        window.minSize = NSSize(width: 980, height: 680)
        window.center()
        window.setFrameAutosaveName("MyAgentMainWindow")
        window.delegate = self

        let config = WKWebViewConfiguration()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        config.websiteDataStore = .default()
        webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.setValue(false, forKey: "drawsBackground")
        window.contentView = webView
        webView.load(URLRequest(url: url))
        window.makeKeyAndOrderFront(nil)
    }

    private func stopCore() {
        core?.terminate()
        if let core, core.isRunning {
            let deadline = Date().addingTimeInterval(2)
            while core.isRunning && Date() < deadline {
                Thread.sleep(forTimeInterval: 0.05)
            }
            if core.isRunning { core.terminate() }
        }
        try? coreLog?.close()
        core = nil
    }

    private func buildMenu() {
        let mainMenu = NSMenu()

        let appItem = NSMenuItem()
        mainMenu.addItem(appItem)
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About MyAgent", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Hide MyAgent", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(withTitle: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        appMenu.items.last?.keyEquivalentModifierMask = [.command, .option]
        appMenu.addItem(withTitle: "Show All", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Quit MyAgent", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu

        let editItem = NSMenuItem()
        mainMenu.addItem(editItem)
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu

        let viewItem = NSMenuItem()
        mainMenu.addItem(viewItem)
        let viewMenu = NSMenu(title: "View")
        let reload = NSMenuItem(title: "Reload", action: #selector(reloadPage), keyEquivalent: "r")
        reload.target = self
        viewMenu.addItem(reload)
        let devTools = NSMenuItem(title: "Toggle Developer Tools", action: #selector(toggleDevTools), keyEquivalent: "i")
        devTools.target = self
        devTools.keyEquivalentModifierMask = [.command, .option]
        viewMenu.addItem(devTools)
        viewItem.submenu = viewMenu

        let windowItem = NSMenuItem()
        mainMenu.addItem(windowItem)
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowItem.submenu = windowMenu
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = mainMenu
    }

    @objc private func reloadPage() {
        if let url = serverURL { webView.load(URLRequest(url: url)) }
    }

    @objc private func toggleDevTools() {
        let selector = Selector(("toggleDeveloperTools:"))
        if webView.responds(to: selector) {
            webView.perform(selector, with: nil)
        }
    }

    private func showFatal(_ error: Error) {
        let alert = NSAlert()
        alert.alertStyle = .critical
        alert.messageText = "MyAgent could not start"
        alert.informativeText = error.localizedDescription
        alert.addButton(withTitle: "Quit")
        alert.runModal()
        NSApp.terminate(nil)
    }

    // Open external links in the default browser instead of inside the app.
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        if let url = navigationAction.request.url,
           let host = url.host,
           host != "127.0.0.1",
           host != "localhost",
           navigationAction.navigationType == .linkActivated {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
            return
        }
        decisionHandler(.allow)
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let alert = NSAlert()
        alert.messageText = "MyAgent"
        alert.informativeText = message
        alert.addButton(withTitle: "OK")
        alert.runModal()
        completionHandler()
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let alert = NSAlert()
        alert.messageText = "MyAgent"
        alert.informativeText = message
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        completionHandler(alert.runModal() == .alertFirstButtonReturn)
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
