---
title: ADR-0109 HTTP 소스의 줄마다 JSON 응답과 레코드 안의 배열 펼치기 — response_format json_lines, records_path의 [*]와 ^., 보인 것만 완결성
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/egress/hxd_ended_test.go
  - backend/internal/httpsource/hxd_lines_test.go
  - backend/internal/httpsource/hxd_expand_test.go
  - backend/internal/ingest/hxd_defs_test.go
  - backend/internal/ingest/hxd_review_test.go
  - backend/internal/httpsource/hxd_review_test.go
  - backend/internal/sourcecheck/httptls_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0092-http-source-adapter-adapter-httpsource.md
  - docs/design-docs/adr/0097-http-source-review-secrets-everywhere-streaming-and-limits.md
  - docs/design-docs/adr/0100-http-source-extension-baseline-basic-token-requests-and-where-secrets-go.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/source-editing.md
  - docs/design-docs/glossary.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0100"
    what: 뒤의 PR로 미뤘던 E4(줄마다 JSON인 응답)와 E8(레코드 안의 배열 펼치기)의 구현과 그 세부 결정
  - adr: "0092"
    what: 응답이 JSON 값 하나라는 전제(response_format json_lines를 더하고, 이때 페이지가 없다)와 records_path의 문법([*])
  - adr: "0097"
    what: 읽기의 단위(줄과 바깥 레코드)와 egress가 알리는 본문의 끝 표시를 완결성에 쓰는 것
amends_tests:
  - go:backend/internal/egress::TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins
  - go:backend/internal/egress::TestHxbTheScanOfTheSettingsFailsForTheWaysAround
  - go:backend/internal/egress::TestHxbTheScanOfTheSettingsFailsWhenAPlaceIsGone
  - go:backend/internal/egress::TestTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/egress::TestTheDialerConnectsToTheLiteralAddressItJudged
  - go:backend/internal/httpsource::TestA408IsSentAgainAndOtherStatusesAreTheRequestsFault
  - go:backend/internal/httpsource::TestA429IsSentAgainAfterTheTimeTheSourceAsksFor
  - go:backend/internal/httpsource::TestABodyThatBreaksWhileItIsReadIsAConnectionThatWasLostAndIsSentAgain
  - go:backend/internal/httpsource::TestABodyThatCannotBeWrittenIsTheProductsFault
  - go:backend/internal/httpsource::TestACancelledReadEndsWithTheErrorOfTheContextAndClosesWhatItOpened
  - go:backend/internal/httpsource::TestACertificateAuthorityThatCannotBeUsedIsAConfigurationError
  - go:backend/internal/httpsource::TestACheckOfASourceThatIsOffOrCancelledOrOfAnIncompleteFirstResponse
  - go:backend/internal/httpsource::TestACheckReadsTheFirstResponseOnlyAndTellsItemByItem
  - go:backend/internal/httpsource::TestACheckSaysWhichItemFailedWithTheKindAndTheWordsOfTheFailure
  - go:backend/internal/httpsource::TestAConnectionThatBreaksOrTimesOutIsSentAgainButACertificateIsNot
  - go:backend/internal/httpsource::TestACursorInTheBodyOfAPostReadsThroughThePages
  - go:backend/internal/httpsource::TestACursorThatDoesNotMoveStopsTheReadAsNotComplete
  - go:backend/internal/httpsource::TestADatasetWithNoPaginationIsOneResponseAndAPathThatIsNoPathIsNoTotalAndNoNext
  - go:backend/internal/httpsource::TestAFailureWithNoRetryAfterIsSentAgainThreeTimesAfterGrowingWaitsThatTheLimitCaps
  - go:backend/internal/httpsource::TestAFullLastPageIsFollowedByAnEmptyOneAndEndsComplete
  - go:backend/internal/httpsource::TestAGetIsTheDefaultAPostNeedsPostReadsAndAPostIsSentOnce
  - go:backend/internal/httpsource::TestAPreviewGivesMaskedRowsAndTheExchangesThatMadeThem
  - go:backend/internal/httpsource::TestAPreviewIsAsBigAsAskedAndHidesWhatTheSourceGaveAsAnAddress
  - go:backend/internal/httpsource::TestAPreviewThatFailsGivesTheErrorAndNoRows
  - go:backend/internal/httpsource::TestAReadGoesOnFromTheTokenOfAnyPageAndGivesTheSameRows
  - go:backend/internal/httpsource::TestAReaderIsMadeForOneSourceAndOneHttpDatasetAndSourcesThatAreOffAreNotRead
  - go:backend/internal/httpsource::TestAReaderThatWasGivenNoSleepWaitsOnATimer
  - go:backend/internal/httpsource::TestAReaderWithNoObserverTellsNobody
  - go:backend/internal/httpsource::TestARecordThatIsNotARowIsUnreadableWithWhereItWas
  - go:backend/internal/httpsource::TestARetryAfterLongerThanTheLimitEndsTheReadAtOnce
  - go:backend/internal/httpsource::TestASecretThatMaySeeNoSuchUseIsTheSettingsFault
  - go:backend/internal/httpsource::TestASecretWhoseStoreFailsIsTheProductsFaultAndOneAtTheWrongAddressIsRefused
  - go:backend/internal/httpsource::TestAShortPageDoesNotEndTheReadWhileTheTotalIsNotReached
  - go:backend/internal/httpsource::TestATokenGoesOnWithTheNumbersOfTheLinesAndTheTotal
  - go:backend/internal/httpsource::TestATokenHoldsPositionsAndNothingElse
  - go:backend/internal/httpsource::TestATokenRequestAsksForATokenOnceAndSendsItOnEveryRequestAfterwards
  - go:backend/internal/httpsource::TestATokenRequestMayBeAFormAndMayChooseTheHeaderAndThePrefix
  - go:backend/internal/httpsource::TestATokenRequestThatFailsIsTheSourcesRefusalOrAFormatError
  - go:backend/internal/httpsource::TestATokenTheSourceNoLongerAcceptsIsAskedForOnceMore
  - go:backend/internal/httpsource::TestATotalThatIsNotWhatWasReadMakesTheReadNotComplete
  - go:backend/internal/httpsource::TestAWaitThatFailsEndsTheReadWithItsError
  - go:backend/internal/httpsource::TestAddressesTheSourceGaveAreNeverPutInAToken
  - go:backend/internal/httpsource::TestAnAdapterThatSignsInNeedsAWayToOpenSecretsAndAClockAndAConnector
  - go:backend/internal/httpsource::TestAnAddressOfAnotherOriginIsNotFollowed
  - go:backend/internal/httpsource::TestAnAddressThatPointsAtItselfStopsTheReadAsNotComplete
  - go:backend/internal/httpsource::TestAnAnswerBiggerThanTheLimitAfterItIsUnpackedIsAConfigurationError
  - go:backend/internal/httpsource::TestAnAnswerThatIsNotWhatTheDefinitionExpectsIsAFormatError
  - go:backend/internal/httpsource::TestAnEmptyDatasetIsOneEmptyCompletePage
  - go:backend/internal/httpsource::TestAnEndTokenReadsNothingAndAnUnusableTokenReadsFromTheStart
  - go:backend/internal/httpsource::TestAnErrorOfTheCallersContextIsNotAFailureOfTheSource
  - go:backend/internal/httpsource::TestEachKindOfAuthenticationSignsTheRequestAndNothingElseDoes
  - go:backend/internal/httpsource::TestEachReadMakesItsOwnClientWithTheTimeoutAndTheCertificateAuthorityOfTheConnection
  - go:backend/internal/httpsource::TestEventsAreToldWhatHappenedAndHaveNoFieldForAValue
  - go:backend/internal/httpsource::TestEveryFailureOfTheConnectionIsOneOfTheClosedKindsWithItsClass
  - go:backend/internal/httpsource::TestEveryKindOfPaginationReadsTheWholeDatasetAndEndsComplete
  - go:backend/internal/httpsource::TestHsfACursorThatHoldsASecretIsNotPutInAToken
  - go:backend/internal/httpsource::TestHsfACursorWithNoSecretStaysInTheToken
  - go:backend/internal/httpsource::TestHsfAPlaceInsideARecordAndABooleanAreReadByThePagination
  - go:backend/internal/httpsource::TestHsfARecordHasAMostOfTwoMebibytes
  - go:backend/internal/httpsource::TestHsfARecordIsNeverGivenTwiceWhenABodyBreaksAndTheRequestIsSentAgain
  - go:backend/internal/httpsource::TestHsfARecordWhoseKeyIsASecretHasNoSecretInItsRow
  - go:backend/internal/httpsource::TestHsfARequestSentAgainAfterABrokenBodyMayFailInItsWaitOrItsAnswer
  - go:backend/internal/httpsource::TestHsfARetryAfterThatOverflowsIsLongerThanTheLimit
  - go:backend/internal/httpsource::TestHsfASecretEchoedIntoTheNextPathIsInNoEventAndNoExchange
  - go:backend/internal/httpsource::TestHsfASecretIsTakenOutOfNumbersAndOfEveryDepthOfARecord
  - go:backend/internal/httpsource::TestHsfATokenIsMadeOncePerPageAndNotOncePerRecord
  - go:backend/internal/httpsource::TestHsfAValueForTheNextPageHasAMostOfEightKibibytes
  - go:backend/internal/httpsource::TestHsfAnAnswerThatIsNotTheShapeIsAFailureAtTheFirstPlaceItIsSeen
  - go:backend/internal/httpsource::TestHsfEchoedSecretsAreTakenOutOfTheRowsOfARunAndOfAPreview
  - go:backend/internal/httpsource::TestHsfEveryTextOfAnExchangeHasTheValuesTakenOut
  - go:backend/internal/httpsource::TestHsfFailuresOfTheShapeInsideTheParts
  - go:backend/internal/httpsource::TestHsfTheAnswerToTheRequestForATokenIsSmallAndWhole
  - go:backend/internal/httpsource::TestHsfTheCursorIsLookedAtAsItIsAndNotOnlyInsideTheToken
  - go:backend/internal/httpsource::TestHsfTheEventOfAResponseHasTheBytesOfTheBodyRead
  - go:backend/internal/httpsource::TestHsfTheMemoryOfAReadDoesNotFollowTheSizeOfTheAnswer
  - go:backend/internal/httpsource::TestHsfThePlacesThePaginationReadsAreFoundWhereverTheyAre
  - go:backend/internal/httpsource::TestHxaABodyHandedBackAsATextHasNoValueOfTheSecretInTheWayJSONWritesIt
  - go:backend/internal/httpsource::TestHxaABodyPlaceholderIsAValueAndTheStructureStaysWhole
  - go:backend/internal/httpsource::TestHxaARequestSentAgainAfterA401CarriesTheNewToken
  - go:backend/internal/httpsource::TestHxaAShortValueOfTheSecretIsNotCarriedInTheTokenOfAPage
  - go:backend/internal/httpsource::TestHxaATokenRequestWithNoBodySendsAnEmptyObject
  - go:backend/internal/httpsource::TestHxaAUserNameInTheDataChangesNoRowAndNoKey
  - go:backend/internal/httpsource::TestHxaAValueForAHeaderIsRefusedBeforeTheRequestForTheToken
  - go:backend/internal/httpsource::TestHxaAValueThatCannotBeAHeaderIsNotSentAndNotRetried
  - go:backend/internal/httpsource::TestHxaBasicCredentialsAndATokenThatIsTheWholeBody
  - go:backend/internal/httpsource::TestHxaEveryFieldOfTheOpenSecretIsTakenOutWhicheverDatasetIsRead
  - go:backend/internal/httpsource::TestHxaTheHeadersOfTheAuthenticationCarryTheSecretAndTheToken
  - go:backend/internal/httpsource::TestHxaTheKindBasicRefusesAUserNameWithAColon
  - go:backend/internal/httpsource::TestHxaWhatAPlaceOfTheSecretGivesIsTakenOutWhenTheSourceHandsItBack
  - go:backend/internal/httpsource::TestHxcACheckTellsTheTokenRequestAsAnItemOfItsOwn
  - go:backend/internal/httpsource::TestHxcALifetimeThatCannotBeReadIsToldOnceInARead
  - go:backend/internal/httpsource::TestHxcAReaderRefusesATokenAddressThatIsNoAddress
  - go:backend/internal/httpsource::TestHxcARequestOfTheAPIThatHasASecretIsRefusedWhereItIsMadeToo
  - go:backend/internal/httpsource::TestHxcARequestThatIsSentAgainAfterABrokenBodyHasATokenThatHolds
  - go:backend/internal/httpsource::TestHxcASecretInARequestOfTheAPIIsRefusedBeforeTheTokenRequest
  - go:backend/internal/httpsource::TestHxcASecretThatDoesNotAllowTheTokenHostSendsNothingAnywhere
  - go:backend/internal/httpsource::TestHxcASignerGivesTheTokenRequestTheCredentialsAndTheAPITheToken
  - go:backend/internal/httpsource::TestHxcATokenIsAskedForAgainBeforeItEndsOnTheClockOfTheReader
  - go:backend/internal/httpsource::TestHxcATokenThatIsTooLongOrCannotBeAHeaderIsRefusedBeforeItIsSent
  - go:backend/internal/httpsource::TestHxcAWholeAddressOfTheAPIsOwnOriginIsTheAPIsHost
  - go:backend/internal/httpsource::TestHxcAfterA401TheTokenIsAskedForAgainAndTheOldOneGoesToNoServerOfTheTwo
  - go:backend/internal/httpsource::TestHxcAnErrorOfTheTokenRequestKeepsItsKindAndDetail
  - go:backend/internal/httpsource::TestHxcEachHostHasItsOwnCertificateAuthority
  - go:backend/internal/httpsource::TestHxcRealEgressACertificateAuthorityThatCannotBeUsedForTheTokenHostIsRefused
  - go:backend/internal/httpsource::TestHxcRealEgressATokenRequestWithoutTLSToAnAddressThatIsNotTheLoopbackIsRefused
  - go:backend/internal/httpsource::TestHxcRealEgressThePolicyJudgesTheTokenHostToo
  - go:backend/internal/httpsource::TestHxcTheAddressOfEachRequestIsItsOwnHost
  - go:backend/internal/httpsource::TestHxcTheClientInABasicHeaderAndTheKeyInAJSONBodyGoToTheTokenHostToo
  - go:backend/internal/httpsource::TestHxcTheClientOfTheTokenHostLivesAsLongAsTheRead
  - go:backend/internal/httpsource::TestHxcTheCredentialsGoToTheTokenHostAndTheTokenToTheAPIOnly
  - go:backend/internal/httpsource::TestHxcTheEventsOfTheTokenRequestSayTheHostItWentTo
  - go:backend/internal/httpsource::TestHxcTheReasonOfMaxPagesSaysHowManyRequestsWereForATokenToo
  - go:backend/internal/httpsource::TestHxcTheSecretIsOpenedForTheAPIAndForTheTokenHost
  - go:backend/internal/httpsource::TestHxcTheTokenRequestIsCountedInTheRequestsOfARead
  - go:backend/internal/httpsource::TestHxcTokensAskedForAgainBecauseOfA401AreWithinABudget
  - go:backend/internal/httpsource::TestLeavingTheLoopClosesTheClientAndTheBodies
  - go:backend/internal/httpsource::TestMaxPagesStopsTheReadAsNotComplete
  - go:backend/internal/httpsource::TestNoSecretValueIsInAnyErrorEventResultOrPrintOfTheReader
  - go:backend/internal/httpsource::TestPageNumbersStartWhereTheDefinitionSays
  - go:backend/internal/httpsource::TestRetryAfterMayBeADate
  - go:backend/internal/httpsource::TestTheBodyOfAPostKeepsNumbersArraysAndBooleansAndACursorThatLooksLikeANumberStaysText
  - go:backend/internal/httpsource::TestTheHeadersOfTheConnectionGoOnEveryRequestAndTheAuthenticationWinsTheSameName
  - go:backend/internal/httpsource::TestTheOtherKindsStopAtOnceOnA401And403AndDoNotAskAgain
  - go:backend/internal/httpsource::TestThePagesAreCutByTheSharedRulesWhateverTheSizeOfAResponse
  - go:backend/internal/httpsource::TestThePlainHeadersOfTheConnectionAreNeverCompressedByTheAdapter
  - go:backend/internal/httpsource::TestTheQueryAndThePathAreMadeFromTheDefinitionWithTheBracesUnescaped
  - go:backend/internal/httpsource::TestTheRecordsMayBeTheWholeDocumentOrNullOrNothing
  - go:backend/internal/httpsource::TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported
  - go:backend/internal/httpsource::TestTheRequestsAreSpacedByTheRateOfTheConnection
  - go:backend/internal/httpsource::TestTheSameAnswerTwiceInARowStopsTheReadAsNotComplete
  - go:backend/internal/httpsource::TestTheSecretIsOpenedForThisSourceAtTheHostAndPortOfTheRequestsAndForThePurposeOfTheRead
  - go:backend/internal/httpsource::TestTheStateOfARunAndTheTokenItHoldsPrintAsNothing
  - go:backend/internal/httpsource::TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted
  - go:backend/internal/httpsource::TestTheWordsOfAnErrorAreMaskedOfTheValuesOfTheOpenSecretsAsASecondWall
  - go:backend/internal/httpsource::TestThroughTheRealEgressClientAnAddressThePolicyRefusesIsAConfigurationErrorAndNothingIsSent
  - go:backend/internal/httpsource::TestWhatIsUnderAnErrorStaysForTheOperatorAndNeverInTheWords
  - go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace
test_change_reason: 단언은 바꾸지 않았다. 지문이 바뀐 이유는 셋이다. (1) 새 hxd_rig_test.go의 가짜 타입이 Do, Read, Close, CloseIdleConnections 메서드를 정의하는데 잠금 도구가 이름으로 도달 범위를 셈하므로 이 메서드를 부르는 옛 rig의 httpsource 테스트들의 지문이 바뀐다. (2) go:embed로 패키지의 .go 파일을 읽어 글자를 검사하는 테스트 7개(egress 5개와 httpsource 2개)는 egress/client.go와 새 테스트 파일이 지문에 든다. (3) 잠긴 사례 표 adpCases에 새 오류 코드의 사례 3개를 더했다(ingest 1개).
decision_hash: 0f1d1ce96be2beb3
---
# ADR-0109 HTTP 소스의 줄마다 JSON 응답과 레코드 안의 배열 펼치기

## 맥락

ADR-0100이 E4(줄마다 JSON인 응답)와 E8(레코드 안의 배열을 행으로 펼치기)의 결정을 적고 구현은 이 PR(A2)로 미뤘다. 사내 데이터의 대부분이 HTTP API이고, 응답의 레코드 안에 배열(호스트 안의 인터페이스, VM 안의 디스크)이 흔하며, 스트리밍으로 내보내는 API는 줄마다 JSON으로 답한다. 지금은 응답이 JSON 하나여야 하고 `[*]`는 표기만 정해 둔 정의 오류라서 이 둘을 읽을 길이 없다. 둘 다 완전성이 틀리면 잘못된 지워짐이 되므로(검토의 R12, R27, R28) 읽는 법과 함께 끝을 어떻게 말하는지를 정해야 한다. ADR-0102가 지은 완전성의 세 갈래(완전, 보인 것만, 불완전)를 처음으로 어댑터가 쓴다.

## 결정

1. **E4. `response_format`.** 데이터셋의 `response_format`은 `json`(기본)이나 `json_lines`다. `json_lines`는 기존 흘려 읽기(`window`, `json.Decoder`) 위에서 줄바꿈이나 다른 공백으로 이어진 JSON 값을 하나씩 읽고, 값 하나가 레코드 하나다. `records_path`는 값 안의 자리이고 비면 값 전체가 레코드다. 빈 줄은 건너뛰고 `Content-Type`은 보지 않는다. 한도는 그대로다(응답 바이트, 레코드 하나 2 MiB, 창 여유 64 KiB). `pagination`은 `none`만 쓸 수 있다.
2. **E4의 끝.** egress의 `Response`에 읽기 전용 필드 `Ended`(본문의 끝을 프로토콜이 표시하는가: 길이, 마지막 청크, HTTP/2)를 더한다. 전송 계층이 풀어 준 압축 본문은 끝 표시로 세지 않는다(길이가 지워지고, gzip은 첫 바이트 전이나 멤버 사이에서 연결이 닫혀도 오류 없이 끝난다. 검토에서 진짜 소켓으로 확인했다). `json_lines` 요청에는 압축하지 말라고 알리고(`egress.Request.Identity`: 어댑터 코드에 `Accept-Encoding`이라는 글을 두지 못하게 잠근 검사가 있어 egress가 붙인다), 그래도 압축된 본문이 오면(`identity`를 요청했으므로 전송 계층은 풀지 않는다) 풀리지 않은 바이트가 JSON이 아니어서 `response_format` 실패다(보인 것만이 아니다). 이유 글에 서버가 압축해 보냈다는 말(`Content-Encoding`)이 나온다(N2). 끝 표시와 함께 끝난 본문은 완전하다(값이 하나도 없는 본문과 내용 없음(204)은 예외로, 보내지 않은 것과 레코드가 없는 것을 가를 수 없어 보인 것만이다). 끝 표시 없이 끝나거나 읽는 중에 끊기면 끝까지 받은 줄만 레코드이고 끊긴 줄은 버리며 그 데이터셋은 **보인 것만**(`Completeness.SeenOnly`)이다. 요청을 다시 보내 건너뛰지 않고 이어 읽기 토큰도 없고 받은 토큰은 쓰지 않는다. 끝 표시가 있는데 값의 가운데에서 끝난 본문, JSON이 아닌 값, `records_path`에 아무것도 없는 줄, 한도 초과는 `response_format` 실패(그 데이터셋을 통째로 뺀다)이고, 객체가 아니거나 자연 키가 없는 레코드가 하나라도 있으면 그 데이터셋은 불완전이다(오류를 한 줄로 붙여 보내는 제품의 끝 줄이 키 없는 레코드로 읽혀 데이터셋을 완전하게 두지 않으려는 것이다). 한도나 형식 오류가 끊김과 함께 오면 실패가 이긴다(끊김이 오류를 작게 만들지 않는다). 호출자가 취소한 읽기는 끊김이 아니라 취소다. 점검은 앞의 100개 레코드만 읽고 본문을 닫는다.
3. **E8. `[*]`와 `^.`.** `records_path`에 `[*]`를 한 번 쓸 수 있다(`result[*].interfaces`). `[*]` 앞은 바깥 레코드가 있는 자리이고 뒤는 바깥 레코드 안의 배열까지의 경로이며, 그 배열의 원소가 이 데이터셋의 레코드다. 두 번, 뒤에 경로 없음, `[*]` 바로 뒤의 글은 정의 오류다(`source.path.invalid`). `json`에서 `[*]` 앞은 바깥 레코드의 배열 자리(비면 응답이 그 배열)이고, `json_lines`에서는 줄 안의 바깥 레코드 자리(비면 줄이 바깥 레코드)다. 원소의 컬럼 경로는 첫 단계의 `^`로 바깥 레코드를 읽고(`^.hostid`) 한 단계 위만이다. 맨 `^`, `^.^.x`, 가운데의 `^`는 정의 오류이고 이름이 `^`인 키는 `["^"]`로 적는다. `[*]`가 없는 데이터셋에서는 `^`가 지금처럼 키 이름이다. 자연 키는 원소의 필드를 하나는 읽어야 하고 숫자 단계(배열 안의 자리)를 읽는 컬럼을 쓸 수 없다(`source.dataset.expand_invalid`). **`ParsePath`는 바꾸지 않는다**: 별도의 읽기 함수(`ParseRecordsPath`, `ParseColumnPath`)가 `records_path`와 컬럼 경로를 읽으므로 잠긴 문법 테스트와 파일 JSON Lines의 옛 경로 읽기가 그대로다. 컬럼 경로의 `[*]`와 모든 `[?…]`는 그대로 "지원하지 않음"이다.
4. **E8의 셈과 한도.** 페이지 넘김, 짧은 페이지, `total_path`와의 견줌, 이어 읽기 토큰의 건너뜀과 앞선 수는 **바깥 레코드**로 센다(원소로 세면 offset이 건너뛰어 바깥 레코드가 빠지고, 전체 수가 없는 소스는 완전으로 끝난다). 바깥 레코드 하나(2 MiB 이하)를 읽어 그 원소를 배열 순서대로 하나씩 페이지에 넣으므로 레코드 하나가 한 페이지를 넘기지 못하고, 바깥 레코드 하나의 원소는 `max_elements`(기본 100,000, 정의가 1부터 1,000,000까지)까지이며 넘으면 `response_format`이다. 페이지가 바깥 레코드의 가운데에서 잘리면 그 페이지의 토큰은 그 바깥 레코드의 처음부터 읽게 하고, 본문이 끊겨 다시 받을 때도 건너뛰는 것은 끝까지 준 바깥 레코드뿐이다. 그래서 다시 받은 응답에서 안쪽 순서가 달라도 빠지는 원소가 없고, 다시 준 원소는 같은 키의 행이 겹칠 뿐이다. 행의 줄 번호는 바깥 레코드의 번호이고 위치는 "page P, record I, element J"다. `^.`로 읽은 바깥 값은 원소마다 행에 실리므로 응답 바이트 한도가 행의 양을 묶지 못한다(1.9 MB의 응답이 58.6 GiB의 행이 된 검토의 재현). 그래서 **응답 하나에서 만든 행의 바이트 합(키와 셀)은 `max_response_bytes`의 4배까지**이고(고정 비율) 넘으면 `response_format` 실패다. 파일의 JSON Lines(어댑터 흐름, `records_path`)에는 파일의 한도가 없으므로 **지금까지 읽은 바이트의 4배에 고정 몫 8 MiB(`DefaultPageBytes`)를 더한 값까지**이고(N1) 넘으면 그 데이터셋은 불완전이다(보인 것만이 아니다. 이유 글에 만든 행이 읽은 양의 4배를 넘었다는 말이 나온다). HTTP와 파일은 같은 상수 `ingest.RowBytesFactor`와 같은 함수 `ingest.RowsOver`를 쓴다. 이어 읽기 토큰은 그 앞에서 배열이 없던 바깥 레코드 수와 찾아본 바깥 레코드 수를 싣고, 이어 읽은 데이터셋은 그 수를 이어 받아 끝을 정한다(이어 읽으면 앞의 "배열 없음"을 잊고 완전으로 끝나던 구멍).
5. **E8의 배열이 없을 때.** 바깥 레코드에 배열이 없거나 null이면 원소 0개이고 그런 레코드가 **하나라도 있으면** 그 데이터셋은 보인 것만이다(없는 배열이 비어서인지 보이지 않아서인지 모르므로 안 보인 것을 세지 않는다. 그러면 경로가 틀려 모두에 배열이 없어도 대량 삭제가 없다). `missing_array: "empty"`를 고르면 원소 0개이고 완전성은 그대로다(`seen_only`는 기본을 이름으로 적는 값이다). 다만 한 캡처에서 바깥 레코드가 하나 이상인데 **그 모두에** 배열이 없으면 `empty`여도 보인 것만으로 끝나고 "배열이 하나도 없음, `records_path`를 확인"이라는 이유의 사건 하나를 남긴다(경로가 틀리면 모든 레코드가 원소 0개로 끝나 데이터셋의 객체가 모두 닫히기 때문이다). 일부에만 없으면 `empty`의 뜻대로다. 배열이 아닌 값(객체, 글, 수, 참거짓), 객체가 아닌 바깥 레코드, 배열까지 가는 중간 단계가 객체가 아닌 값(null과 없는 키는 배열 없음), 객체가 아닌 원소는 `response_format` 실패다. `json_lines`와 펼치는 데이터셋에서 레코드가 하나 이상 있었는데 행이 하나도 안 나오면(`ignore_rows_without_key` 등으로 모두 건너뜀) 보인 것만이고 사건 하나를 남긴다. 설계 6절의 "모두에 없으면 불완전"은 7절의 이 규칙으로 대체됐다.
6. **보인 것만을 내는 곳.** 어댑터(`adapter.httpsource`)가 마지막 페이지에서 `Completeness{SeenOnly: true}`를 낸다(`readloop.go`의 `endOf`). 더 약한 끝이 이긴다: 페이지 한도나 전체 수 불일치(불완전)가 보인 것만보다 앞이고, 읽을 수 없는 레코드(`json_lines`)가 그 다음이다. 파일의 JSON Lines는 `pagestream.go`의 `endOf`가 낸다.
7. **파일의 JSON Lines.** 어댑터 흐름(`ParseAdapterSource`)의 `parser: json_lines` 데이터셋은 `records_path`, `missing_array`, `max_elements`를 받고 같은 뜻으로 읽는다. 줄이 바깥 레코드이고(`records_path`의 `[*]` 앞이 있으면 줄 안의 그 자리), 원소 하나가 한 레코드 단위로 페이지에 들어가며, 배열이 없는 줄이 있으면 보인 것만이고 원소가 한도를 넘으면 불완전이다. `records_path`를 쓰는 파일은 읽을 수 없는 줄(JSON이 아님, `records_path`에 값이 없음, 다른 종류의 값, 객체가 아니거나 자연 키가 없는 원소)이 하나라도 있으면 HTTP의 `json_lines`와 같이 불완전이고, "모두 없음"의 가드는 배열이 있던 줄(빈 배열도)이 0개인가로 판단한다. 옛 파일 흐름(`ParseSource`, `Parse`, `Runner`)은 이 필드를 정의 오류로 거절하거나 읽지 않는다(아래 "옛 흐름").
8. **권한 분류.** `response_format`, `missing_array`는 매핑 변경(레코드가 어디에 있고 읽은 것이 전부인지 어떻게 말하는가이며 새 목적지, 시크릿, 요청을 더하지 않는다), `max_elements`는 한도라서 올리는 변경(기본 100,000보다 큰 값으로 새 데이터셋을 더하는 것 포함)은 접속 변경이고 내리는 변경은 매핑 변경이다(`ClassifyChange`의 분류표와 한도 비교 `limitCompared`).
9. **새 정의 오류 코드 셋.** `source.response_format_invalid`, `source.response_format_pagination`, `source.dataset.expand_invalid`(그 밖은 기존 `source.path.invalid`, `source.path.unsupported`, `source.dataset.field_not_for_adapter`). 코드마다 잠긴 사례 표(`adpCases`)에 사례를 더했다. 실행 때의 일은 새 오류 종류를 만들지 않고 기존 `SourceError` 종류(`response_format`)와 고정된 글로 말한다.

## 이유

- 끝 표시를 보지 않으면 줄 경계에서 끊긴 본문이 깨끗한 끝과 같아서(검토 X2) 평문 연결의 중간자나 서버의 비정상 종료가 데이터셋을 완전으로 끝내고 삭제를 낳는다. 반대로 끝 표시가 없다고 모두 실패로 하면 쓸 수 있는 줄도 버리게 되므로, 읽은 줄은 쓰되 안 보인 것을 세지 않는 세 번째 끝(보인 것만)이 맞다.
- 다시 받아 건너뛰기는 줄 순서가 정해지지 않는 질의에서 다른 줄을 건너뛰고 다른 줄을 두 번 주므로 `json_lines`에서는 하지 않는다. 바깥 레코드 경계로 내려서 읽는 것은 같은 이유다(검토 R30).
- `ParsePath`를 바꾸지 않는 것은 그 문법이 모든 어댑터의 컬럼 경로와 파일 JSON Lines의 옛 정의가 읽는 하나의 문법이고, `^`가 지금도 유효한 키 이름이기 때문이다(검토 R31).

## 대안과 기각 사유

- 끝 표시를 egress에 싣지 않고 헤더의 `Content-Length`만 보기: 줄마다 JSON을 내보내는 API는 대개 청크로 답하므로 모두 보인 것만이 된다. 기각.
- 읽을 수 없는 줄을 건너뛰고 세기: 조용한 누락이다. 기각(설계 5절 Q2).
- 원소 수로 페이지를 세기: offset이 건너뛴다. 기각.
- `["^"]`를 바깥 참조의 다른 표기로 허용하기: 키 이름 `^`와 겹친다. 키 이름으로만 쓴다.

## 판단을 적어 둔 곳(설계 책임자가 보는 것)

(가) egress의 `Response`에 읽기 전용 필드 `Ended`를 더했다. 지시서의 "egress 변경 없음"은 정책과 TLS를 가리킨다고 보았으나 본문의 끝 표시는 그곳에서만 알 수 있어 더한 것이다. 방어 코드(정책, 다이얼러, TLS, 우회 검사)는 바꾸지 않았다. (나) `["^"]`는 바깥 참조가 아니라 이름이 `^`인 키다. (다) `json_lines`에서 `[*]` 앞은 줄 안의 바깥 레코드 자리다(줄이 바깥 레코드). (라) 읽을 수 없는 레코드가 있는 `json_lines`는 데이터셋 오류가 아니라 불완전한 끝으로 낸다(읽은 페이지는 이미 나갔고 끝에서 불완전이라 통째로 빠진다). (마) `missing_array: "empty"`에서 경로가 틀려 모든 바깥 레코드에 배열이 없는 경우는 보인 것만으로 끝낸다(5번). 소스가 빈 배열을 생략한다고 아는 정의만 `empty`를 고르게 하는 것이 첫 방어이고 이것이 두 번째다. (바) `Preparer`는 보인 것만의 끝도 `incomplete_capture` 문제로 적는다(ADR-0102의 구현 그대로이고 이 PR이 바꾸지 않는다). (사) 모든 행이 건너뛰어지면 보인 것만으로 한다(검토 R28의 덧붙임. 6번). `error_path`는 하지 않았다(어디에도 없는 필드다).

## 옛 흐름

`ingest.Runner`는 `Fetcher`가 준 `Capture`(`Complete`는 불리언)를 읽고 `Completeness`를 받지 않으므로 `SeenOnly`를 받는 길이 없다. 파일의 `records_path`를 `ParseSource`가 거절하고 `Parse`와 `ParseLimited`가 읽지 않으므로(줄을 레코드로 읽어 자연 키가 없는 줄로 알린다) 옛 흐름에서 삭제가 늘어나는 길은 없다. 확인하는 테스트는 `hxd_defs_test.go`의 두 테스트다.

## 남는 위험

`json_lines`가 보인 것만으로 끝나는 본문을 계속 내는 소스는 삭제가 유예되어 데이터셋이 늙는다(안 보인 것을 세지 않는다). 끝 표시가 있는데 소스가 값의 일부를 빠뜨리고 끝내는 경우는 어댑터가 알 수 없다. 파일 JSON Lines의 행 한도는 읽은 바이트 기준이라(파일에는 한도가 없다) 작은 파일도 고정 몫 8 MiB까지의 행을 만들 수 있다. 이것은 설계 책임자가 정한 값이다.

알려진 한계(독립 검토에서 나온 낮은 것들). 빈 파일 JSON Lines는 완전이다. 파일은 끝까지 읽혔음이 분명하고, 대량 삭제는 수집 흐름의 확인이 막는다. HTTP `json_lines`의 빈 본문은 끊김과 가를 수 없어 보인 것만이다. 레코드 사이의 공백이 2 MiB를 넘으면 오류 글이 "레코드가 2 MiB보다 큼"으로 나온다(메모리는 묶인다). 점검은 원소가 없는 줄만 이어지면 응답 바이트 한도까지 읽는다. 자연 키 검사는 이름이 숫자인 키(`m["0"]`)를 배열 안의 자리로 보고, 바깥 필드가 없는 키(두 호스트의 `eth0`이 겹침)는 정의 때 알 수 없다. 어댑터 흐름의 파일 JSON Lines에 `records_path`를 더하면 컬럼 경로를 새 문법으로 읽고(옛 점 나누기와 다를 수 있다) 실행 때에야 드러난다. `max_elements: 0`은 오류 없이 기본값이다.

## 범위 밖

E7(앞 요청의 결과, PR C), egress 정책과 시크릿 변경, PostgreSQL의 배열(`sql`로 이미 된다), `error_path`(검토의 선택 항목), 커버리지 하한의 손 편집.

## 호환의 변화

- `response_format`, `missing_array`, `max_elements`는 새 필드이고 없으면 옛 정의와 같다. `records_path`가 `[*]`를 받아들이는 것은 옛 "지원하지 않음" 오류를 없앤다(그 오류를 단언하던 잠긴 테스트는 컬럼 경로의 `[*]`와 `[?…]`를 단언하므로 그대로다).
- 이어 읽기 토큰의 지문은 `records_path`를 이미 담고 있어 `[*]`를 쓰는 정의의 토큰은 옛 토큰과 섞이지 않는다.
