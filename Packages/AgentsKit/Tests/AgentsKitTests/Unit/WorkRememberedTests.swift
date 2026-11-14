import Foundation
import Testing
@testable import AgentsKitCore

/// Each Work page opens with what it had when last left (#5451), as workRemembered.test.mjs has it on the web page.
struct WorkRememberedTests {
    @Test func aListsFiltersAndSortAreTheWebPagesWordsAndComeBack() {
        let f = WorkList.Filter(state: .closed, project: "agents", label: "bug")
        let words = WorkRemembered.words(f, sort: .newest, kind: .issue)
        #expect(words == "project=agents&state=closed&label=bug&sort=newest")
        let back = WorkRemembered.list(words, kind: .issue)
        #expect(back.filter == f)
        #expect(back.sort == .newest)
    }

    @Test func theDefaultsAreNoWords() {
        #expect(WorkRemembered.words(.init(), sort: .updated, kind: .issue) == "")
        #expect(WorkRemembered.words(.init(), sort: .newest, kind: .release) == "")
        #expect(WorkRemembered.words(.init(text: "  "), sort: .updated, kind: .pr) == "")
        let none = WorkRemembered.list(nil, kind: .release)
        #expect(none.filter == .init())
        #expect(none.sort == .newest)
    }

    @Test func aStateSortOrBoardAPageDoesNotHaveIsTheDefault() {
        let got = WorkRemembered.list("state=merged&sort=updated&board=x&label=y", kind: .release)
        #expect(got.filter.state == .open)
        #expect(got.sort == .newest)
        #expect(got.filter.board == "")
        #expect(got.filter.label == "y")
        #expect(WorkRemembered.list("state=merged", kind: .pr).filter.state == .merged)
    }

    @Test func wordsWithSpacesPlusAndAmpersandComeBack() {
        let f = WorkList.Filter(label: "c++ & go", text: "a b=c")
        let words = WorkRemembered.words(f, sort: .updated, kind: .issue)
        #expect(!words.contains("+"))
        #expect(WorkRemembered.list(words, kind: .issue).filter == f)
        // The web page's URLSearchParams writes a space as "+".
        #expect(WorkRemembered.list("q=a+b", kind: .issue).filter.text == "a b")
        #expect(WorkRemembered.list("%zz", kind: .issue).filter == .init())
    }

    @Test func featuresAndTimelineKeepTheirChoices() {
        #expect(WorkRemembered.features(board: "") == "")
        let board = WorkRemembered.features(board: "https://github.com/orgs/a/projects/1")
        #expect(WorkRemembered.value("board", in: board) == "https://github.com/orgs/a/projects/1")
        #expect(WorkRemembered.timeline(range: .twoDays, project: "", board: "") == "")
        let t = WorkRemembered.timeline(WorkRemembered.timeline(range: .week, project: "p1", board: "b"))
        #expect(t.range == .week)
        #expect(t.project == "p1")
        #expect(t.board == "b")
        #expect(WorkRemembered.timeline("range=year").range == .twoDays)
    }

    @Test func eachPageHasItsOwnKey() {
        #expect(WorkRemembered.key(.issue) == "agents.work.filters.issue")
        #expect(WorkRemembered.key(.feature) == "agents.work.filters.features")
        #expect(WorkRemembered.key(.timeline) == "agents.work.filters.timeline")
    }
}
