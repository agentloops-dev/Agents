import AgentsKitCore
import SwiftUI

/// A Work page's trail (#5452): *Features › Agent loops › Epic #5281 › #5291*. Each part but the
/// last is a plain button in the secondary colour that opens its place; the last, the page
/// itself, is semibold. Never wraps: a long feature's name is cut in its middle.
struct WorkTrailView: View {
    let model: any WorkModel
    let parts: [WorkTrail.Part]

    var body: some View {
        HStack(spacing: 4) {
            ForEach(Array(parts.enumerated()), id: \.offset) { i, part in
                if i > 0 {
                    Image(icon: "chevron.compact.right").foregroundStyle(.tertiary).accessibilityHidden(true)
                }
                if let target = part.target {
                    Button(part.words) { model.openTrail(target) }
                        .buttonStyle(.plain)
                        .foregroundStyle(.secondary)
                        .truncationMode(.middle)
                        .help(Self.help(target, part.words))
                } else {
                    Text(part.words).fontWeight(.semibold).monospacedDigit().layoutPriority(1)
                }
            }
        }
        .appText(.reading)
        .lineLimit(1)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Place")
    }

    /// The tip: what the part opens.
    static func help(_ target: WorkTrail.Target, _ words: String) -> String {
        switch target {
        case .list(let kind): "All \(kind.title.lowercased())"
        case .feature: "\(words) in Features"
        case .item: "The page of \(words)"
        }
    }
}
