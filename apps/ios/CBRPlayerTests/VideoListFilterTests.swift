import Testing
@testable import CBRPlayer

@MainActor
struct VideoListFilterTests {

    private func video(_ name: String) -> VideoItemModel {
        VideoItemModel(id: name, name: name, hlsURL: "/videos/\(name)/playlist.m3u8",
                       createdAt: nil, duration: 60, resolution: nil, sizeMB: 1)
    }

    private func model(_ names: [String]) -> VideoListViewModel {
        let vm = VideoListViewModel()
        vm.videos = names.map(video)
        return vm
    }

    @Test func emptyFilterShowsAllSortedByName() {
        let vm = model(["charlie", "Alice_part2", "bob", "alice_part1"])
        #expect(vm.filteredFiles.map(\.name) == ["alice_part1", "Alice_part2", "bob", "charlie"])
    }

    @Test func filterMatchesSubstringIgnoringCase() {
        let vm = model(["alice_2026-11-01_part1", "bob_2026-11-01_part1", "ALICE_2026-11-02_part1"])
        vm.filterText = "Alice"
        #expect(vm.filteredFiles.map(\.name) == ["alice_2026-11-01_part1", "ALICE_2026-11-02_part1"])

        vm.filterText = "11-02"
        #expect(vm.filteredFiles.map(\.name) == ["ALICE_2026-11-02_part1"])
    }

    @Test func filterIgnoresSurroundingWhitespace() {
        let vm = model(["alice", "bob"])
        vm.filterText = "  bob \n"
        #expect(vm.filteredFiles.map(\.name) == ["bob"])

        vm.filterText = "   "
        #expect(vm.filteredFiles.count == 2)
    }

    @Test func noMatchesGivesEmptyList() {
        let vm = model(["alice", "bob"])
        vm.filterText = "zzz"
        #expect(vm.filteredFiles.isEmpty)
    }
}
