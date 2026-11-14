import Foundation

/// Each Work page opens with the filters, sort and range it had when the person last left it
/// (#5451). What is kept is in the words of the web page's address ("project=agents&label=bug&sort=newest",
/// `workQuery` in `model/work.ts`), each left out while it is its default, so the three clients keep the same.
/// The window and the Remote keep it in defaults, under `key(_:)`; the web page in localStorage, under the same name.
public enum WorkRemembered {
    /// What a page is kept under: `agents.work.filters.issue`, `.pr`, `.release`, `.features` or `.timeline`.
    public static func key(_ kind: WorkList.Kind) -> String {
        "agents.work.filters.\(kind == .feature ? "features" : kind.rawValue)"
    }

    /// A list's filters and sort as words; "" when all are the defaults.
    public static func words(_ filter: WorkList.Filter, sort: WorkList.Sort, kind: WorkList.Kind) -> String {
        let pairs: [(String, String)] = [
            ("q", filter.text.trimmingCharacters(in: .whitespaces).isEmpty ? "" : filter.text),
            ("project", filter.project),
            ("state", filter.state == .open ? "" : filter.state.rawValue),
            ("repo", filter.repo),
            ("label", filter.label),
            ("assignee", filter.assignee),
            ("board", filter.board),
            ("sort", sort == kind.defaultSort ? "" : sort.rawValue),
        ]
        return words(pairs)
    }

    /// A list's filters and sort from words; what they leave out, or a word the kind does not have, is the default.
    public static func list(_ words: String?, kind: WorkList.Kind) -> (filter: WorkList.Filter, sort: WorkList.Sort) {
        let q = values(words)
        let state = q["state"].flatMap(WorkList.State.init(rawValue:)).flatMap { kind.states.contains($0) ? $0 : nil } ?? .open
        let sort = q["sort"].flatMap(WorkList.Sort.init(rawValue:)).flatMap { kind.sorts.contains($0) ? $0 : nil } ?? kind.defaultSort
        let filter = WorkList.Filter(state: state, project: q["project"] ?? "", repo: q["repo"] ?? "", label: q["label"] ?? "",
                                     assignee: q["assignee"] ?? "", text: q["q"] ?? "",
                                     board: kind == .issue ? q["board"] ?? "" : "")
        return (filter, sort)
    }

    /// Work ▸ Features's board as words.
    public static func features(board: String) -> String { words([("board", board)]) }

    /// Work ▸ Timeline's range, project and board as words.
    public static func timeline(range: WorkTimeline.Range, project: String, board: String) -> String {
        words([("range", range == .twoDays ? "" : range.rawValue), ("project", project), ("board", board)])
    }

    /// Work ▸ Timeline's range, project and board from words; the defaults for what they leave out.
    public static func timeline(_ words: String?) -> (range: WorkTimeline.Range, project: String, board: String) {
        let q = values(words)
        return (q["range"].flatMap(WorkTimeline.Range.init(rawValue:)) ?? .twoDays, q["project"] ?? "", q["board"] ?? "")
    }

    /// One value from words.
    public static func value(_ name: String, in words: String?) -> String { values(words)[name] ?? "" }

    private static func words(_ pairs: [(String, String)]) -> String {
        var c = URLComponents()
        c.queryItems = pairs.filter { !$0.1.isEmpty }.map { URLQueryItem(name: $0.0, value: $0.1) }
        // URLComponents leaves "+" as it is, which the web page's URLSearchParams reads as a space.
        return (c.percentEncodedQuery ?? "").replacingOccurrences(of: "+", with: "%2B")
    }

    private static func values(_ words: String?) -> [String: String] {
        // As URLSearchParams: "+" is a space. Words that are not a query give nothing.
        guard let words, let c = URLComponents(string: "?" + words.replacingOccurrences(of: "+", with: "%20")) else { return [:] }
        var got: [String: String] = [:]
        for item in c.queryItems ?? [] where got[item.name] == nil { got[item.name] = item.value ?? "" }
        return got
    }
}
