---
title: ADR-0111 HTTP 소스의 자식 데이터셋 — 앞 요청의 결과를 쓰는 요청(E7), 두 단계 읽기, 건너뜀의 규칙과 한도
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/hxe_defs_test.go
  - backend/internal/httpsource/hxe_child_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0092-http-source-adapter-adapter-httpsource.md
  - docs/design-docs/adr/0100-http-source-extension-baseline-basic-token-requests-and-where-secrets-go.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/glossary.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0100"
    what: 뒤의 PR로 미뤘던 E7(앞 요청의 결과를 쓰는 요청)의 구현과 그 세부 결정(건너뜀의 규칙, 한도의 상한, 부모 값의 저장)
  - adr: "0092"
    what: 요청 틀에 변수가 없다는 전제(`{parent.<이름>}` 하나를 더한다)와 데이터셋 전체의 요청 수와 시간에 한도가 없다는 미정(자식과 실행의 한도를 더한다)
amends_tests:
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
  - go:backend/internal/httpsource::TestHxdABodyOfLinesWithNoValueIsOnlyWhatWasSeen
  - go:backend/internal/httpsource::TestHxdABodyThatBreaksGivesTheLinesThatWereReadAndIsNotSentAgain
  - go:backend/internal/httpsource::TestHxdABodyThatBreaksInsideARecordLosesNoElement
  - go:backend/internal/httpsource::TestHxdABodyWithNoMarkOfItsEndIsOnlyWhatWasSeen
  - go:backend/internal/httpsource::TestHxdACanceledReadOfLinesIsNotACut
  - go:backend/internal/httpsource::TestHxdACheckReadsTheFirstLinesOnly
  - go:backend/internal/httpsource::TestHxdACutLastLineIsLeftOutWithNoMarkAndAFailureWithOne
  - go:backend/internal/httpsource::TestHxdAFaultOfTheShapeWinsOverABreak
  - go:backend/internal/httpsource::TestHxdALineThatCannotBeReadMakesTheDatasetNotComplete
  - go:backend/internal/httpsource::TestHxdAPageIsCutInsideARecordAndTheTokenGoesOnFromItsStart
  - go:backend/internal/httpsource::TestHxdAPreviewReadsLines
  - go:backend/internal/httpsource::TestHxdAReadThatGoesOnFromATokenKnowsTheRecordsBeforeItThatHadNoArray
  - go:backend/internal/httpsource::TestHxdARecordOfAnotherKindIsNotARecordWithNoArray
  - go:backend/internal/httpsource::TestHxdARecordWithMoreElementsThanTheLimitFails
  - go:backend/internal/httpsource::TestHxdARecordWithNoArrayMakesTheDatasetOnlyWhatWasSeen
  - go:backend/internal/httpsource::TestHxdATokenIsNotUsedForLines
  - go:backend/internal/httpsource::TestHxdAnElementThatIsNoObjectFailsAndRecordsThatMadeNoRowAreOnlyWhatWasSeen
  - go:backend/internal/httpsource::TestHxdJSONLinesAreRecordsAndTheReadIsOneRequestWithNoToken
  - go:backend/internal/httpsource::TestHxdLinesWithARecordWithNoArrayAreOnlyWhatWasSeen
  - go:backend/internal/httpsource::TestHxdMissingArrayEmptyDoesNotMakeADatasetOfRecordsWithNoArrayAtAllComplete
  - go:backend/internal/httpsource::TestHxdMissingArrayEmptyKeepsTheDatasetCompleteAndAValueThatIsNoArrayFails
  - go:backend/internal/httpsource::TestHxdNoContentIsNoRecordsForLines
  - go:backend/internal/httpsource::TestHxdOneRecordWithManyElementsIsCutIntoPages
  - go:backend/internal/httpsource::TestHxdTheElementsOfTheArraysAreTheRecordsInTheOrderOfTheAnswer
  - go:backend/internal/httpsource::TestHxdTheLimitsOfTheBodyHoldForLines
  - go:backend/internal/httpsource::TestHxdTheOuterRecordIsTheLineForLinesAndTheArrayOfThemForAValue
  - go:backend/internal/httpsource::TestHxdThePagesAreCountedByTheOuterRecords
  - go:backend/internal/httpsource::TestHxdTheRecordOfALineIsAtRecordsPathAndALineWithNothingThereFailsTheDataset
  - go:backend/internal/httpsource::TestHxdTheRowsOfOneResponseAreBoundedByFourTimesTheBytesOfTheResponseLimit
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
  - go:backend/internal/httpsource::TestHxeABatchOfValuesThatJSONWritesLongerFitsTheBody
  - go:backend/internal/httpsource::TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead
  - go:backend/internal/httpsource::TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported
  - go:backend/internal/httpsource::TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted
  - go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace
  - go:backend/internal/ingest::TestCheckFirstPageReadsThePageAndSaysHowItWentOut
  - go:backend/internal/ingest::TestRdfTheCoverageOfAReadIsOneOfThreeAndAValueThatSaysBothIsReadAsIncompleteAndAHeaderThatChangedLeavesTheDatasetOut
test_change_reason: 단언은 바꾸지 않았다. 지문이 바뀐 이유는 둘이다. (1) 읽는 쪽 코드가 잠금 도구의 도달 범위 안에 있어서(자식 읽기, 한도, 요청마다 `charge`를 세는 것, `Reader`의 새 필드와 `openSigner`와 `needs`의 시그니처) httpsource의 옛 테스트 160개의 지문이 바뀌고, 새 hxe_rig_test.go의 가짜 타입이 Do, Read, Close 메서드를 정의하는 것도 이름으로 도달 범위를 세는 도구에서는 같은 영향을 준다(둘을 따로 가르지는 않았다. 예전 ADR-0109가 같은 모양으로 적었다). (2) 잠긴 사례 표 adpCases에 새 오류 코드의 사례 아홉 개를 더했고(사전 승인된 변경), 이 표를 읽는 ingest의 잠긴 테스트 셋의 지문이 바뀐다. 어느 단언도 바뀌지 않았고 어느 테스트도 사라지지 않았다. (3) 검토 R1·R2(테스트 보강)와 R5(가)에 따라 `TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead`의 표에 목록 꼴 오류 원소 세 줄을 더했고(기대 seen), 빈 목록이 모든 값에 온 행의 기대를 complete에서 seen으로 바꿨다(R2 기준은 값이 셋을 넘는데 레코드가 하나도 없으면 보인 것만, 이 ADR 6항). `TestHxeABatchOfValuesThatJSONWritesLongerFitsTheBody`는 본문 한도를 1000에서 4000 바이트로, 값을 10개에서 40개로 바꿨다(3배 추정이 통과하던 것을 막으려고, 단언은 그대로). (4) `TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported`와 `TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted`는 httpsource의 제품 코드 전체를 글자로 읽는 테스트라, 이 PR이 고친 `child.go`, `parentscan.go`, `readloop.go`, `send.go`의 지문이 바뀌었다. 단언과 금지 목록은 바뀌지 않았고 새 코드에는 금지된 글자가 없다.
decision_hash: 4b97d5b3f9ce7dd4
---
# ADR-0111 HTTP 소스의 자식 데이터셋 — 앞 요청의 결과를 쓰는 요청(E7)

## 맥락

ADR-0100이 E7(앞 요청의 결과가 필요한 요청)의 결정을 적고 구현은 이 PR(C)로 미뤘다. id 목록을 주는 API와 id 하나의 상세를 주는 API가 따로인 소스가 많아서, 선언만으로는 상세를 읽을 수 없었다(`sources-and-adapters.md` 10절의 "안 돼요"). 설계 7절과 Q14가 정의의 모양, 읽는 순서, 한도를 정했고 검토(R20부터 R26)가 막을 것을 적었다. 이 ADR은 구현의 결정과, 설계 책임자가 중간 보고에 답한 판단을 적는다.

## 결정

1. **정의.** 데이터셋은 `collect: false`(보조. 값만 주고 행으로 받지 않는다)이거나 `parent` 블록(`dataset`, `values`, 선택 `batch`, `max_requests`, `max_seconds`)을 가진 자식일 수 있다. 보조 데이터셋은 `columns`, `natural_key`, `deletion`, `ignore_rows_without_key`를 적으면 정의 오류(`source.collect_helper_mapped`)이고 삭제 규칙은 `none`으로 정해진다. 부모는 하나이고 깊이는 하나이며 순환과 알 수 없는 부모는 정의 오류다. 새 정의 오류 코드는 아홉이다(`source.parent_invalid`, `parent_unknown`, `parent_depth`, `parent_cycle`, `parent_template_unknown`, `parent_template_place`, `parent_value_unused`, `source.batch_invalid`, `source.collect_helper_mapped`). 잠긴 표 `adpCases`에 사례 아홉 개를 더했다(사전 승인된 변경).
2. **자리표시.** `{parent.<이름>}`(한 겹 중괄호)을 요청 경로의 조각 안, 질의 값, 본문 값 통째에만 쓴다. `{{`와 `}}`는 그대로 중괄호다(잠긴 `TestAdpDoubledBracesStandForTheBrace`가 지킨다). 헤더, 이름과 키, 묶음 변수를 경로나 글 일부에 쓰는 것, 다른 글과 섞인 본문 변수는 정의 오류다. 헤더는 중괄호를 검사하지 않는다(헤더 값은 정의가 쓴 글 그대로이고 시크릿은 따로 간다. 이 PR이 바꾸지 않았다).
3. **두 단계 읽기.** 부모를 자기 정의대로(페이지 넘김 포함) 끝까지 읽어 값만 모으고(중복 없이, 처음 본 순서) 그다음 자식을 단위(값 하나, 또는 묶음 하나)마다 읽는다. 부모 본문이 열려 있는 동안 자식 요청을 보내지 않는다(검토 X5의 회귀). 부모 읽기는 요청 하나의 시간보다 길어도 된다. 자식은 단위마다 처음 페이지부터 읽고 이어 읽기 토큰을 내지 않으며 받은 토큰도 쓰지 않는다. 자식의 응답이 객체 하나이면 레코드 하나로 읽는다(`records_path`가 가리키는 자리, 비어 있으면 응답 전체가 객체인 자식에 한해서). 위치는 `httpsource/child.go`의 `readChild`, `parentscan.go`의 `scanParent`, `unit.go`다.
4. **경로의 인코딩.** 경로는 `u.Path`에 원문, `u.RawPath`에 값마다 `PathEscape`한 글을 두어 값이 정확히 한 번 이스케이프된다. 본문의 값은 `json.Number`나 글자 그대로의 값으로만 들어가 부모 값이 `{{auth.token}}` 꼴이어도 토큰으로 읽히지 않는다. 부모 값은 비었거나 256바이트를 넘거나, ASCII 출력 글자가 아니거나, 앞뒤에 공백이 있거나, `/`, `\`, `.`, `..`, 인코딩된 구분자(`%2f`, `%2e`, `%5c`), 묶음 구분자가 들었거나, `;` 앞이 `.`나 `..`이면 보내지 않는다. 값은 ASCII만 받는다(그 밖의 글자를 받는 소스는 아직 없고, 넓히는 것은 쉽지만 좁히는 것은 어렵다).
5. **묶음.** `parent.batch`(`size` 기본 100과 최대 1,000, `separator`는 `,;|:` 가운데, `match`는 필수)는 값 여럿을 질의나 본문의 변수 하나에 묶는다. 묶음은 크기와 함께 주소 8 KiB, 본문 1 MiB를 넘지 않게 자른다. 묶음의 404는 항상 불완전이다. 묶음에서 `match`로 가리킨 값이 응답에 없는 것은 세어서 그 데이터셋을 보인 것만으로 끝낸다.
6. **완결성(설계 책임자의 결정 2와 3).** 404와, 규칙에 걸려 보내지 않은 부모 값을 **같은 규칙**으로 다룬다. 읽지 못한 값(404를 건너뛴 값, 규칙에 걸려 보내지 않은 값, 응답이 객체 하나인데 자연 키 열이 하나라도 없어 행이 못 된 값. 마지막은 `ignore_rows_without_key`와 무관하게 센다)의 수가 서로 다른 부모 값의 수(규칙에 걸린 값을 포함한다. 규칙에 걸린 값은 레코드마다 세고, 묶음에서 객체 하나 꼴 응답이 행을 못 만들면 값이 아니라 단위(요청)마다 센다. 결과는 늘 보인 것만 이상이다)의 5%를 넘으면서 3건을 넘으면 그 데이터셋은 불완전이고, 그보다 적으면 보인 것만이다. 자식이 레코드를 읽었는데 행이 하나도 안 되면 최상위 데이터셋처럼 보인 것만이다(`seenOnly()`의 안전망). 값마다 목록을 돌려주는 자식의 빈 배열은 정상(자식이 없는 것)이다. 다만 값이 3개를 넘는데 어느 응답에도 레코드가 하나도 없으면(모든 값에 `null`, 204, `[]`, 빈 목록이 온 것) 권한이나 경로가 잘못된 것일 수 있어 보인 것만이다(`kid.end`). 값이 하나도 없는 부모는 최상위의 빈 응답과 같게 완전이다. 건너뛴 값마다 문제 `source.parent_value_invalid`(값 자체는 싣지 않는다)를 세어 사건 하나로 알린다. 불완전은 데이터셋을 통째로 빼서 갱신까지 멈추므로 값 하나 때문에 그렇게 되지 않게 하려는 것이다. 삭제는 두 경우 모두 일어나지 않는다. 부모가 불완전하거나, 값이 상한을 넘거나, 404이거나, 한도에 걸리면 자식 요청은 0건이고 불완전이다. 부모가 보인 것만이면 자식도 보인 것만이다. 404는 `on_not_found: "skip"`일 때만 건너뛰고(묶음은 안 된다) 아니면 불완전이다. 수신은 보조 데이터셋을 건너뛰고(`ingest/intake.go`, `ingest/run.go`) 불완전한 자식은 통째로 뺀다.
7. **한도(Q14)와 실행의 예산.** 한도의 상한은 `httpsource.Config.Ceilings`(기본값은 아래 표)이고 서버의 플래그는 서버를 조립하는 PR에서 연결한다. `Reader`는 실행마다 새로 만들고(문서 주석에 적었다), 실행의 예산(요청 수, 시작 시각)은 `Reader`가 하나 가진다. 같은 `Reader`로 두 번 읽으면 예산이 이어진다는 것을 테스트가 묶는다. 모든 요청(부모, 자식, 토큰 요청)은 `once()` 안의 `run.charge()`가 센다. 실행 시간의 한도는 자식을 읽을 때만 적용한다(아래 이유). 요청 수 한도는 모든 읽기에 걸린다. `parent.max_requests`에는 부모와 토큰 요청도 들어간다(값이 정확히 `max_requests`개이면 부모 요청 때문에 항상 불완전이므로 여유를 두고 정한다). 토큰 요청의 404는 값이 없는 것이 아니므로 건너뛰지 않는다(읽기 오류). 점검의 첫 자식 요청이 404이면 `on_not_found: skip`이어도 설정 문제로 알린다.
8. **점검과 미리 보기.** 점검은 부모의 첫 값만 읽어 자식 요청 한 건을, 미리 보기는 자식 요청 열 건까지 보낸다. 건수는 단위가 아니라 실제로 보낸 자식 요청(페이지와 다시 보낸 것 포함, 부모와 토큰 요청은 제외)이다(`limits.go`의 `chargeChild`). 두 경우 모두 필요한 값만 얻으면 부모 읽기를 멈추고 부모 요청도 한도에 센다. 부모를 읽는 요청은 미리 보기와 점검에서 스무 건까지만 보내며(`previewParentRequests`, 값이 두 개만 번갈아 오거나 끝없이 이어지는 부모를 끝까지 읽지 않으려는 것이다), 그때까지 모은 값으로 자식을 읽는다. 값이 하나도 없으면 자식 요청은 0건이고 그렇다고 알린다. 결과 문구에 "첫 값만 읽음"을 적는다.
9. **부모 값의 저장.** 모은 값은 50만 개 이하이면서 64 MiB 이하여야 한다. 64 MiB는 값의 글자 수가 아니라 값마다 고정 덧셈(`parentValueOverhead` 96바이트: 맵과 슬라이스의 머리와 맵 항목)을 더한 센 바이트다. 그리고 `max_requests` × 묶음 크기가 더 작으면 그것이 상한이다. 넘으면 자식 요청 0건이고 불완전이다.
10. **권한 분류.** `collect`, `parent`, `on_not_found`는 무엇을 소스에 묻는지를 정하므로 접속 변경(`classKept`)이다. `max_requests`, `max_seconds`, `batch.size`를 낮추기만 하는 변경은 매핑 변경이고 올리는 변경은 접속 변경(이유 `datasets.<id>.parent_limits`)이다(`ingest/change.go`의 `parentFolded`).
11. **관찰.** 사건과 교환 기록은 값을 싣지 않고 경로를 `/v1/vm/{parent.id}`처럼 변수의 꼴로 적는다. 새 사건 `EventSkipped`와 미리 보기의 `Note`를 더했다. 시크릿 값은 부모 값이어도 주소에 넣지 않는다(가림 목록이 먼저 걸린다).

한도의 표와 세는 곳.

| 한도 | 기본 | 상한 | 세는 곳 |
|---|---|---|---|
| 자식 요청 수 | 2,000 | 50,000 | `limits.go`의 `childBudget`, `run.charge()` |
| 실행의 요청 수 | | 200,000 | `budget.requests`, `run.charge()` |
| 자식 시간 | 30분 | 4시간 | `childBudget.deadline`(주입 시계) |
| 실행 시간 | 2시간 | 6시간 | `budget.start`(자식을 읽을 때만) |
| 동시성 | 1 | 1 | 한 읽기 안에서 차례로 |
| 묶음 크기 | 100 | 1,000 | `chunks`, `batchOf` |
| 부모 값 | | 50만 개, 64 MiB | `scanParent` |
| 값 길이 | | 256바이트 | `validParentText` |
| 주소, 본문 | | 8 KiB, 1 MiB | `unit.path`, `build`, `batchRoom` |
| 요청 속도 | 초당 5 | | 기존 속도 조절기 |
| 미리 보기, 점검 | | 10건, 1건 | `readUnits`, `valuesNeeded` |

## 이유

- 부모를 끝까지 읽은 뒤에 자식을 읽으면 부모 본문을 연 채 요청을 보내지 않아 연결과 메모리를 묶지 않고, 부모가 한도에 걸린 것을 자식 요청 전에 알아 요청이 하나도 나가지 않는다.
- 건너뜀을 보인 것만으로 끝내는 것은 값 하나(지워졌거나 권한이 없는 것)로 데이터셋이 통째로 빠지고 갱신이 멈추는 것을 피하려는 것이다. 5%와 3건을 넘으면 경로나 권한이 잘못된 것으로 본다.
- 실행 시간의 한도를 자식에만 거는 것은 잠긴 `TestHxcATokenIsAskedForAgainBeforeItEndsOnTheClockOfTheReader`가 시계를 하루 건너뛰어 토큰의 수명을 테스트하기 때문이다. 요청 수의 한도는 모든 읽기에 걸린다. 자식이 없는 읽기의 시간은 요청 한도, 페이지 한도, 요청 시간이 이미 묶는다.
- `Ceilings`를 읽기 도구의 설정에 두고 서버의 플래그를 뒤로 미루는 것은 서버 조립이 이 PR의 범위가 아니기 때문이다.

## 대안과 기각 사유

- 부모를 읽으면서 자식을 보내기(흘려 읽기): 부모 본문이 열린 채 요청이 나가 연결을 묶고 한도의 계산이 어긋난다(X5). 기각.
- 404를 모두 불완전으로 두기: 값 하나가 지워진 것만으로 데이터셋이 빠지고 갱신이 멈춘다. `on_not_found: "skip"`으로 정의가 밝힐 때만 건너뛴다.
- 규칙에 걸린 부모 값을 정의 오류나 불완전로 두기: 같은 이유로 기각. 404와 같은 규칙으로 센다.
- 부모 값을 DB에 쓰기: 새 저장소와 정리의 문제가 생긴다. 메모리에서 수와 바이트로 묶는다.
- 비ASCII 값 허용: 정규화(NFC 등)와 경로의 이스케이프 규칙을 더 정해야 하고 쓰는 소스가 없다. 기각.

## 판단을 적어 둔 곳(설계 책임자가 보는 것)

(가) 설계 책임자가 정한 것(중간 보고의 1부터 15): 규칙에 걸린 부모 값은 404와 같은 규칙(6), 보인 것만의 기준(6), 한도는 `Config.Ceilings`(7), Reader마다 실행 예산 하나(7), 값의 수와 바이트 둘 다(9), 점검과 미리 보기는 첫 값만(8)이다.
(나) 이 PR이 정한 것: 실행 시간은 자식을 읽을 때만 센다(위 이유). 응답이 객체 하나인 자식은 레코드 하나다(3). 값은 ASCII만 받는다(4). 헤더는 중괄호를 검사하지 않는다(2).
(다) `Reader`는 실행마다 새로 만든다. 같은 `Reader`로 두 번 읽으면 예산이 이어진다는 것을 `TestHxeTheLimitsCountEveryRequestOnTheClockOfTheReader`가 묶는다.
(라) 잠긴 테스트의 단언은 바꾸지 않았다. 지문이 바뀐 테스트는 `amends_tests`에 있고, 이유는 `test_change_reason`에 있다.

## 옛 흐름

파일 소스와 PostgreSQL 소스는 자식을 쓰지 않는다. `collect`, `parent`, `on_not_found`는 HTTP 어댑터의 필드이고 다른 어댑터에 적으면 `source.dataset.field_not_for_adapter`다. 옛 정의(필드가 없는 정의)는 이전과 같이 읽힌다.

## 남는 위험

- 값 하나마다 요청하는 자식은 값이 많으면 느리고 소스에 부담이다. 묶음 조회 엔드포인트가 있으면 그것을 쓰라고 문서에 적었다. 한도 안에서 불완전으로 끝난다.
- 부모가 읽는 사이에 바뀌어 자식이 읽은 값과 어긋날 수 있다. 값 하나는 404나 새 값의 누락으로 나타나고, 건너뜀의 규칙이 데이터셋 전체를 빼지 않으며 삭제는 일어나지 않는다.
- 건너뜀의 규칙은 값이 건너뛴 만큼 안 보이는 것을 "보인 것만"으로 남긴다. 삭제가 유예되는 만큼 데이터셋이 늙는다(ADR-0109와 같다).
- 모든 값의 응답이 빈 성공(204, `null`, `[]`, 빈 목록)이면 보인 것만이다. 최상위 데이터셋은 같은 응답을 완전으로 읽는다(`{"items":null}`, 204, `[]`가 완전이다. ADR-0109의 빈 응답 규칙). 권한이 사흘 이어 잘못된 경우 큰 지워짐은 확인 뒤 적용(I-8)의 안전장치가 맡는다.
- 앞뒤 공백이 흔한 부모 값(공백이 든 이름 등)은 보내지 않으므로 값의 5%를 넘어 자식이 불완전이 될 수 있다. 안전한 쪽의 변화다.

## 이어서 할 일(독립 검토의 결과)

- 수신에 `Reader`를 잇는 PR은 실행마다 새 `Reader`를 만들고, 그것(같은 실행의 데이터셋들은 예산을 나누고 다음 실행은 새로 시작함)을 테스트로 묶는다. 지금은 제품 코드가 `Reader`를 쓰지 않아 코드는 그대로 둔다.
- 보조 데이터셋을 수신이 건너뛰는 것과 보인 것만 자식이 삭제를 막는 것의 PostgreSQL 종단 테스트는 같은 PR로 미룬다(지금은 단위 테스트뿐이다).
- 헤더 값의 `{parent.<이름>}`은 정의 오류(`source.parent_template_place`)다. (앞뒤 공백과 `;` 앞의 `.`·`..`은 4항에서 이미 보내지 않는다.)

## 범위 밖

웹훅, 엑셀, egress의 방어 규칙, 서버 플래그의 연결(서버 조립 PR), 자식의 자식, 한 데이터셋의 부모 둘, 커버리지 하한의 손 편집.

## 호환의 변화

`collect`, `parent`, `on_not_found`는 새 필드이고 없으면 옛 정의와 같다. 요청 틀의 `{parent.<이름>}`은 옛 정의에서 정의 오류였던 글이다. 읽는 쪽의 `Config.Ceilings`는 없으면 기본값이다.
