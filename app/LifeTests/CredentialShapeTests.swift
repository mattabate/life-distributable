import XCTest
@testable import Life

/// The two paste buttons on Settings only accept their own credential's
/// shape (the decider code pasted into Token would lock the phone out of
/// the hub).
final class CredentialShapeTests: XCTestCase {
    func testTokenIs48Hex() {
        let token = String(repeating: "a1", count: 24)
        XCTAssertTrue(SettingsView.looksLikeToken(token))
        XCTAssertFalse(SettingsView.looksLikeToken(String(token.dropLast())))
        XCTAssertFalse(SettingsView.looksLikeToken(token + "\n"))
        XCTAssertFalse(SettingsView.looksLikeToken("ABCDE-FGHJK-MNPQR-STVWX"))
    }

    func testDeciderCodeIsFourGroupsOfFive() {
        XCTAssertTrue(SettingsView.looksLikeDeciderCode("ABCDE-FGHJK-MNPQR-STVWX"))
        XCTAssertTrue(SettingsView.looksLikeDeciderCode(HubClient.normalizeDecider("abcde–fghjk-mnpqr-stvwx ")))
        XCTAssertFalse(SettingsView.looksLikeDeciderCode("ABCDE-FGHJK-MNPQR"))
        XCTAssertFalse(SettingsView.looksLikeDeciderCode("ABCDI-FGHJK-MNPQR-STVWX")) // I is not in the alphabet
        XCTAssertFalse(SettingsView.looksLikeDeciderCode(String(repeating: "a1", count: 24)))
    }
}
