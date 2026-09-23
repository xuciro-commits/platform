// swift-tools-version: 6.0

import PackageDescription

// A Swift implementation of the kernel contract, checked by the shared
// conformance vectors in ../vectors. It is one implementation, not the
// definition of the kernel (Docs/ADR/0002-kernel-as-contract.md).
let package = Package(
    name: "KernelContract",
    platforms: [.macOS(.v14), .iOS(.v17)],
    products: [.library(name: "KernelModel", targets: ["KernelModel"])],
    targets: [
        .target(name: "KernelModel"),
        .testTarget(
            name: "KernelConformanceTests",
            dependencies: ["KernelModel"]
        )
    ]
)
