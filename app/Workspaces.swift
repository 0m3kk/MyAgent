import Foundation

struct WorkspaceProfile: Codable {
    let id: String
    var name: String
    var rootPath: String
    var dataDir: String
    var createdAt: Int64
    var lastOpenedAt: Int64

    enum CodingKeys: String, CodingKey {
        case id, name
        case rootPath = "root_path"
        case dataDir = "data_dir"
        case createdAt = "created_at"
        case lastOpenedAt = "last_opened_at"
    }
}

struct WorkspaceRegistry: Codable {
    var version: Int
    var active: String
    var workspaces: [WorkspaceProfile]
}

enum WorkspaceError: LocalizedError {
    case message(String)

    var errorDescription: String? {
        switch self {
        case .message(let text): return text
        }
    }
}

/// Reads the profile registry used by the native shell. The Go core owns
/// creating/updating profiles; this type only needs to pick the active one and
/// migrate the first-run data layout.
final class WorkspaceManager {
    let home: URL
    let registryURL: URL
    private var registry: WorkspaceRegistry

    init() throws {
        let env = ProcessInfo.processInfo.environment
        if let path = env["MYAGENT_HOME"], !path.isEmpty {
            home = URL(fileURLWithPath: path).standardizedFileURL
        } else {
            home = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Application Support/MyAgent")
                .standardizedFileURL
        }
        registryURL = home.appendingPathComponent("workspaces.json")
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)

        if let loaded = try Self.loadRegistry(home: home, registryURL: registryURL) {
            registry = loaded
        } else {
            registry = try Self.bootstrap(home: home)
            try save()
        }
    }

    var active: WorkspaceProfile {
        registry.workspaces.first(where: { $0.id == registry.active })
            ?? registry.workspaces[0]
    }

    func reload() throws {
        guard let loaded = try Self.loadRegistry(home: home, registryURL: registryURL) else {
            throw WorkspaceError.message("workspaces.json is missing")
        }
        registry = loaded
    }

    private func save() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = try encoder.encode(registry)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        let tmp = home.appendingPathComponent(".workspaces.json.tmp")
        try data.write(to: tmp, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: tmp.path)
        if FileManager.default.fileExists(atPath: registryURL.path) {
            _ = try FileManager.default.replaceItemAt(registryURL, withItemAt: tmp)
        } else {
            try FileManager.default.moveItem(at: tmp, to: registryURL)
        }
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: registryURL.path)
    }

    private static func loadRegistry(home: URL, registryURL: URL) throws -> WorkspaceRegistry? {
        guard FileManager.default.fileExists(atPath: registryURL.path) else { return nil }
        let data = try Data(contentsOf: registryURL)
        var reg: WorkspaceRegistry
        do {
            reg = try JSONDecoder().decode(WorkspaceRegistry.self, from: data)
        } catch {
            throw WorkspaceError.message("Cannot read \(registryURL.path): \(error.localizedDescription)")
        }
        if reg.version == 0 { reg.version = 1 }
        if reg.workspaces.isEmpty { return nil }
        for i in reg.workspaces.indices {
            reg.workspaces[i].dataDir = absolutePath(home, reg.workspaces[i].dataDir)
            reg.workspaces[i].rootPath = absolutePath(home, reg.workspaces[i].rootPath)
            if reg.workspaces[i].dataDir.isEmpty {
                reg.workspaces[i].dataDir = home.appendingPathComponent("profiles/\(reg.workspaces[i].id)").path
            }
            if reg.workspaces[i].rootPath.isEmpty {
                reg.workspaces[i].rootPath = FileManager.default.homeDirectoryForCurrentUser
                    .appendingPathComponent("MyAgent/\(slug(reg.workspaces[i].name))").path
            }
            if reg.workspaces[i].name.isEmpty {
                reg.workspaces[i].name = "Workspace"
            }
        }
        if !reg.workspaces.contains(where: { $0.id == reg.active }) {
            reg.active = reg.workspaces[0].id
        }
        return reg
    }

    private static func bootstrap(home: URL) throws -> WorkspaceRegistry {
        let fm = FileManager.default
        let env = ProcessInfo.processInfo.environment
        let legacy = env["MYAGENT_DATA_DIR"].flatMap { path -> URL? in
            guard !path.isEmpty else { return nil }
            return URL(fileURLWithPath: path).standardizedFileURL
        } ?? home
        let root = env["MYAGENT_WORKSPACE"].flatMap { path -> URL? in
            guard !path.isEmpty else { return nil }
            return URL(fileURLWithPath: path).standardizedFileURL
        } ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("MyAgent")
        let name = env["MYAGENT_WORKSPACE_NAME"].flatMap { $0.isEmpty ? nil : $0 } ?? "Personal"

        let profileDir: URL
        if legacy.path == home.path {
            profileDir = home.appendingPathComponent("profiles/default")
            try fm.createDirectory(at: profileDir, withIntermediateDirectories: true)
            try migrateLegacyData(from: home, to: profileDir)
        } else {
            profileDir = legacy
            try fm.createDirectory(at: profileDir, withIntermediateDirectories: true)
        }
        try? fm.createDirectory(at: root, withIntermediateDirectories: true)
        try? fm.removeItem(at: home.appendingPathComponent("api.json"))

        let now = Int64(Date().timeIntervalSince1970 * 1000)
        let profile = WorkspaceProfile(
            id: "default",
            name: name,
            rootPath: root.path,
            dataDir: profileDir.path,
            createdAt: now,
            lastOpenedAt: now
        )
        return WorkspaceRegistry(version: 1, active: profile.id, workspaces: [profile])
    }

    private static func migrateLegacyData(from old: URL, to new: URL) throws {
        let fm = FileManager.default
        let names = [
            "myagent.db", "myagent.db-wal", "myagent.db-shm", "master.key",
            "artifacts", "tools", "tmp", "agentd.log",
        ]
        for name in names {
            let src = old.appendingPathComponent(name)
            let dst = new.appendingPathComponent(name)
            guard fm.fileExists(atPath: src.path), !fm.fileExists(atPath: dst.path) else {
                continue
            }
            try fm.moveItem(at: src, to: dst)
        }
    }

    private static func absolutePath(_ home: URL, _ path: String) -> String {
        var value = path.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.isEmpty { return "" }
        value = (value as NSString).expandingTildeInPath
        if !value.hasPrefix("/") {
            value = home.appendingPathComponent(value).path
        }
        return URL(fileURLWithPath: value).standardizedFileURL.path
    }

    private static func slug(_ name: String) -> String {
        let allowed = CharacterSet.alphanumerics
        let mapped = name.lowercased().unicodeScalars.map { scalar -> Character in
            allowed.contains(scalar) ? Character(String(scalar)) : "-"
        }
        let value = String(mapped).trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        return value.isEmpty ? "workspace" : value
    }
}
