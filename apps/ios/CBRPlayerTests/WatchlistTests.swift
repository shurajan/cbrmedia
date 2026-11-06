import Foundation
import Testing
@testable import CBRPlayer

/// Фейковый сервис: хранит список в памяти, умеет падать по запросу.
@MainActor
final class FakeWatchlistService: WatchlistServiceProtocol {
    var entries: [WatchEntryModel] = []
    var failNext: String?
    private(set) var added: [String] = []
    private(set) var removed: [String] = []
    /// Вызывается во время add — имитирует действия пользователя, пока сервер ждёт проверку.
    var onAdd: ((WatchlistViewModel) -> Void)?
    weak var viewModel: WatchlistViewModel?

    private func failure() -> NetworkServiceError? {
        defer { failNext = nil }
        return failNext.map { NetworkServiceError.serverError(message: $0) }
    }

    func fetch(host: String, token: String) async -> Result<[WatchEntryModel], Error> {
        if let error = failure() { return .failure(error) }
        return .success(entries)
    }

    func add(user: String, host: String, token: String) async -> Result<Void, Error> {
        if let error = failure() { return .failure(error) }
        added.append(user)
        if let viewModel { onAdd?(viewModel) }
        entries.removeAll { $0.user == user }
        entries.append(.make(user))
        return .success(())
    }

    func remove(user: String, host: String, token: String) async -> Result<Void, Error> {
        if let error = failure() { return .failure(error) }
        removed.append(user)
        entries.removeAll { $0.user == user }
        return .success(())
    }
}

extension WatchEntryModel {
    static func make(_ user: String, enabled: Bool = true, reason: String? = nil,
                     room: String? = nil, recording: Bool = false, updatedAt: String? = nil) -> WatchEntryModel {
        WatchEntryModel(user: user, addedAt: "2026-11-05T10:00:00Z", addedBy: "api", enabled: enabled,
                        disabledReason: reason, roomStatus: room, recording: recording, updatedAt: updatedAt)
    }
}

struct WatchEntryModelTests {

    @Test func decodesAPIResponse() throws {
        let json = """
        [{"user":"alice","addedAt":"2026-11-05T10:00:00Z","addedBy":"telegram","enabled":true,
          "roomStatus":"public","recording":true,"updatedAt":"2026-11-05T10:01:00Z"},
         {"user":"bob","addedAt":"2026-11-05T10:00:00Z","addedBy":"api","enabled":false,
          "disabledReason":"status API returned HTTP 404","recording":false}]
        """
        let entries = try JSONDecoder().decode([WatchEntryModel].self, from: Data(json.utf8))
        #expect(entries.map(\.user) == ["alice", "bob"])
        #expect(entries[0].status == .recording(roomStatus: "public"))
        #expect(entries[1].status == .disabled(reason: "status API returned HTTP 404"))
        #expect(entries[0].addedDate != nil)
    }

    @Test func statusMapping() {
        #expect(WatchEntryModel.make("a").status == .waiting)
        #expect(WatchEntryModel.make("a", room: "offline", updatedAt: "x").status == .watching(roomStatus: "offline"))
        #expect(WatchEntryModel.make("a", room: "blocked", updatedAt: "x").status == .blocked)
        #expect(WatchEntryModel.make("a", room: "not found", updatedAt: "x").status == .notFound)
        // Выключенный важнее остального состояния.
        #expect(WatchEntryModel.make("a", enabled: false, reason: "r", recording: true).status == .disabled(reason: "r"))
    }
}

@MainActor
struct WatchlistViewModelTests {

    @Test func addTrimsNameAndClearsField() async {
        let service = FakeWatchlistService()
        let vm = WatchlistViewModel(service: service)
        vm.newUser = "  alice \n"
        #expect(vm.canAdd)

        await vm.addNewUser(host: "h", token: "t")

        #expect(service.added == ["alice"])
        #expect(vm.newUser.isEmpty)
        #expect(vm.entries.map(\.user) == ["alice"])
        #expect(vm.actionError == nil)
    }

    @Test func addFailureKeepsInputAndShowsError() async {
        let service = FakeWatchlistService()
        service.failNext = "user already exists"
        let vm = WatchlistViewModel(service: service)
        vm.newUser = "alice"

        await vm.addNewUser(host: "h", token: "t")

        #expect(vm.newUser == "alice")
        #expect(vm.actionError == "user already exists")
    }

    @Test(arguments: [
        ("alice", "alice"),
        ("  alice \n", "alice"),
        ("https://someadress.com/alice", "alice"),
        ("https://someadress.com/alice/", "alice"),
        ("https://someadress.com/female/alice/", "alice"),
        ("https://someadress.com/alice/?tab=1#bio", "alice"),
        ("someadress.com/alice/", "alice"),
    ])
    func userNameFromInput(input: String, expected: String) {
        #expect(WatchlistViewModel.userName(from: input) == expected)
    }

    @Test(arguments: ["", "  ", "https://someadress.com", "https://someadress.com/"])
    func noUserNameInInput(input: String) {
        #expect(WatchlistViewModel.userName(from: input) == nil)
    }

    @Test func addByLinkSendsOnlyUserName() async {
        let service = FakeWatchlistService()
        let vm = WatchlistViewModel(service: service)
        vm.newUser = "https://someadress.com/female/bob/"
        #expect(vm.linkHint == "bob")
        #expect(vm.canAdd)

        await vm.addNewUser(host: "h", token: "t")

        #expect(service.added == ["bob"])
        #expect(vm.newUser.isEmpty)
        #expect(vm.checkingUser == nil)
    }

    @Test func plainNameHasNoLinkHint() {
        let vm = WatchlistViewModel(service: FakeWatchlistService())
        vm.newUser = "alice"
        #expect(vm.linkHint == nil)
    }

    /// Отклонённый пользователь, которого фоновое обновление успело показать, исчезает сразу.
    @Test func rejectedAddReloadsList() async {
        let service = FakeWatchlistService()
        let vm = WatchlistViewModel(service: service)
        vm.entries = [.make("ghost")] // показан фоновым обновлением во время проверки
        service.failNext = "User ghost not found, not added"
        vm.newUser = "ghost"

        await vm.addNewUser(host: "h", token: "t")

        #expect(vm.actionError == "User ghost not found, not added")
        #expect(vm.entries.isEmpty)
        #expect(vm.newUser == "ghost")
    }

    /// Текст, введённый пока шла проверка, не стирается успешным ответом.
    @Test func addKeepsTextTypedDuringCheck() async {
        let service = FakeWatchlistService()
        service.onAdd = { vm in vm.newUser = "next" }
        let vm = WatchlistViewModel(service: service)
        service.viewModel = vm
        vm.newUser = "alice"

        await vm.addNewUser(host: "h", token: "t")

        #expect(service.added == ["alice"])
        #expect(vm.newUser == "next")
    }

    @Test func blankInputCannotBeAdded() {
        let vm = WatchlistViewModel(service: FakeWatchlistService())
        vm.newUser = "   "
        #expect(!vm.canAdd)
    }

    @Test func deleteAndEnable() async {
        let service = FakeWatchlistService()
        service.entries = [.make("alice"), .make("bob", enabled: false, reason: "HTTP 404")]
        let vm = WatchlistViewModel(service: service)
        await vm.load(host: "h", token: "t")

        await vm.delete(vm.entries[0], host: "h", token: "t")
        #expect(service.removed == ["alice"])
        #expect(vm.entries.map(\.user) == ["bob"])

        await vm.enable(vm.entries[0], host: "h", token: "t")
        #expect(service.added == ["bob"])
        #expect(vm.entries.first?.enabled == true)
    }

    @Test func silentReloadFailureKeepsList() async {
        let service = FakeWatchlistService()
        service.entries = [.make("alice")]
        let vm = WatchlistViewModel(service: service)
        await vm.load(host: "h", token: "t")

        service.failNext = "network down"
        await vm.load(host: "h", token: "t", silent: true)
        #expect(vm.entries.map(\.user) == ["alice"])
        #expect(vm.loadError == nil)

        service.failNext = "network down"
        await vm.load(host: "h", token: "t")
        #expect(vm.loadError == "network down")
    }
}
