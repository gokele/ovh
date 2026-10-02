// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "OvhConsole",
    platforms: [.iOS(.v17)],
    targets: [
        .executableTarget(
            name: "OvhConsole",
            path: "Sources"
        ),
    ]
)
