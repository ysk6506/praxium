---
title: ADR-0113 질의 스키마 — 등급마다의 스키마와 역할, 뷰와 함수, 등급을 거르는 한 곳
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - backend/internal/pgschema/qsc_grade_test.go
  - backend/internal/pgschema/qsc_grade_internal_test.go
  - backend/internal/pgschema/qsc_refuse_test.go
  - backend/internal/model/qsc_model_test.go
  - backend/internal/pgstore/qsc_query_test.go
  - backend/internal/pgstore/qsc_oracle_test.go
  - backend/internal/pgstore/qsc_gaps_test.go
  - backend/internal/pgstore/qsc_past_test.go
  - backend/internal/pgstore/qsc_schema_test.go
  - backend/internal/pgstore/qsc_installer_test.go
touches_protected:
  - docs/design-docs/adr/0018-secrets-and-encryption.md
  - docs/design-docs/adr/0085-new-kg-storage-implementation-decisions-and-the-schema-plan-unit.md
  - docs/design-docs/adr/0087-schema-executor-and-shared-tables.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-model-storage.md
  - docs/product-specs/requirements-query-api.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/query-schema.md
  - docs/design-docs/storage-shape.md
  - docs/design-docs/postgres-operations.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/design-docs/kg-model.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-model-storage.md
  - docs/product-specs/requirements-query-api.md
amends:
  - adr: "0018"
    what: 식별 키가 될 수 없는 속성이 confidential만이 아니라 open이 아닌 것(restricted 포함)이라는 것 — 키의 값은 등급이 없는 identifier 뷰에 나온다
  - adr: "0085"
    what: 생성기 버전 8의 단계(고정 테이블 넷)와 스키마 계획이 질의 스키마의 뷰, 함수, 설명, 권한도 낸다는 것
  - adr: "0087"
    what: 실행기가 질의 스키마의 역할을 만들고 지우며 공용 마이그레이션 0008이 설치 토큰을 둔다는 것
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgingest::TestExpAStatementThatFailsInAUnitLeavesTheUnitUndoneAndTheRunGoesOnFromIt
  - go:backend/internal/pgingest::TestExpAnObjectIsOnlyACandidateForRetirementWhenAFeedThatRetiresMadeIt
  - go:backend/internal/pgingest::TestExpAnObjectThatARecordOfAnotherSourceStandsUnderIsNotACandidateAndOneThatNoRecordStandsUnderIs
  - go:backend/internal/pgingest::TestExpTheCandidatesForRetirementAreTheObjectsTheOldPipelineRetiresInTheSameCaptures
  - go:backend/internal/pgingest::TestExpTheExpansionDoesNotBeginOnARunWhoseIdentificationIsNotDoneAndTheComputationOfEverythingGoesByUnitsOfRecords
  - go:backend/internal/pgingest::TestExpTheExpansionOfARunOfAWorldThatHasACaseForEveryRule
  - go:backend/internal/pgingest::TestExpTheSetIsTheSameForAnyOrderOfTheRowsReceivedAndOfTheNumbersOfTheRecords
  - go:backend/internal/pgingest::TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords
  - go:backend/internal/pgingest::TestHkaASignalIsMarkedOnceOnEveryRowOfTheSourceAndNeverTwice
  - go:backend/internal/pgingest::TestIdnAChainOfRecordsInAnyOrderIsOneObjectWhenTheTurnsEndBeforeItsEndAndGoEndsTheSearchAndNothingIsLeftWhenItCannotHoldIt
  - go:backend/internal/pgingest::TestIdnAChainOfRecordsLongerThanTheTurnsOfTheDefaultIsOneObjectAndTheTurnsItTakesGrowMoreSlowlyThanItsLength
  - go:backend/internal/pgingest::TestIdnAChainThatIsLeftAndARecordOfALaterRoundThatSharesAKeyWithItAreLeftTogetherAndThenMadeOneObject
  - go:backend/internal/pgingest::TestIdnAGroupOfSeveralObjectsIsReadOnceForEachKeyAndWhatAUnitReadsOfItGrowsWithTheGroupAndNoFaster
  - go:backend/internal/pgingest::TestIdnARecordThatGivesNoKeyKeepsItsClaimsOnceAndIsNotReadAgainFromItsCellsWhenItCameInTheRowsReceived
  - go:backend/internal/pgingest::TestIdnARecordThatRepeatsItsKeyIsIdentifiedAsTheRecordItIsAndMakesOneObjectNotTwo
  - go:backend/internal/pgingest::TestIdnARecordTheComparisonClosedOrLetGoOfTakesNoPartAndMakesNoObjectKeyOrIdentifierWhileTheOtherRecordsAreIdentified
  - go:backend/internal/pgingest::TestIdnARecordWhoseReferenceIsNotSolvedIsReadAgainFromItsCellsAndBoundWithoutTouchingTheOnesThatAre
  - go:backend/internal/pgingest::TestIdnARoundWhoseSearchIsStoppedByItsLimitBindsNoGroupThatIsNotWholeAndLeavesTheRecordsOfItUnresolvedAndCounted
  - go:backend/internal/pgingest::TestIdnARunThatDoesNotOwnTheTablesOfTheComputationNeitherBeginsNorGoesOnWithTheIdentification
  - go:backend/internal/pgingest::TestIdnARunThatHasFewerRoundsThanTheKeysNeedLeavesTheRecordsUnresolvedAndSaysSo
  - go:backend/internal/pgingest::TestIdnARunThatStopsGoesOnFromTheNextUnitAndMakesWhatARunThatDoesNotStopMakes
  - go:backend/internal/pgingest::TestIdnARunThatStopsWhileGoEndsTheSearchForTheGroupsGoesOnAndMakesWhatARunThatDoesNotStopMakes
  - go:backend/internal/pgingest::TestIdnAnObjectWhoseKeyHasAParentIsMadeOnceTheParentIsAndItsIdComesFromGo
  - go:backend/internal/pgingest::TestIdnAnUnresolvedRecordOfASourceThatHasNoPlanInTheRunIsLeftAloneAndCountedAndDoesNotStopTheRunOfAnotherSource
  - go:backend/internal/pgingest::TestIdnAnUnresolvedRecordOfAnotherSourceIsIdentifiedByTheFeedsOfItsOwnSourceAndFindsTheObjectOfThisRun
  - go:backend/internal/pgingest::TestIdnAnUnresolvedRecordOfAnotherSourceWhoseFeedMakesNoObjectAndFindsNoneStaysUnresolvedWhileTheFeedOfThisSourceThatMakesObjectsDoesNotRead
  - go:backend/internal/pgingest::TestIdnEveryStatementOfAUnitReadsTheRegistryByItsIndexesAndWhatItReadsOfItDoesNotGrowWithTheSource
  - go:backend/internal/pgingest::TestIdnEveryStatementOfAUnitThatFailsLeavesTheRunWhereItWasAndTheRunGoesOnToTheSameEnd
  - go:backend/internal/pgingest::TestIdnEveryStatementOfTheClaimsAndOfTheLookupThatFailsIsReportedAndTheNextRunIsTheSame
  - go:backend/internal/pgingest::TestIdnMeasureTheIdentificationOfAGroupOfManyObjects
  - go:backend/internal/pgingest::TestIdnMeasureTheIdentificationOfARunWithAChangeOfOneInTen
  - go:backend/internal/pgingest::TestIdnTheCallsOfTheIdentificationThatAreMadeOutOfOrderOrOnARunThereIsNotAreRefused
  - go:backend/internal/pgingest::TestIdnTheEndsOfTheLinksAndTheClaimsOfARecordGoErasedFromItsCellsAreKeptWithTheKeys
  - go:backend/internal/pgingest::TestIdnTheFeedsOfTheIdentificationAreNamedBySourceAndNumberAndTheRunsOwnSourceMustHaveSome
  - go:backend/internal/pgingest::TestIdnTheIdentificationOfARunThatHasBegunOrWhoseComparisonIsNotDoneIsNotBegunAndWhatItMadeIsNotChanged
  - go:backend/internal/pgingest::TestIdnTheKeysOfARecordFindNoObjectOneObjectOrSeveralAndTheGroupsAreMadeWhateverTheOrder
  - go:backend/internal/pgingest::TestIdnTheLabelsOfTheTurnsBeforeTheLastTwoAreTakenAwayAndTheRowsOfThemAreAtMostTwoForEachPair
  - go:backend/internal/pgingest::TestIdnTheObjectsTheIdentificationMakesAreTheObjectsTheOldPipelineMakesForTheSameCaptures
  - go:backend/internal/pgingest::TestIdnTheRecordThatNamesAnObjectWhenTheRecordsGiveTheSameKeyIsTheOneWhoseDatasetAndKeyComeFirstWhateverTheOrderOfTheRows
  - go:backend/internal/pgingest::TestIdnTheRecordThatNamesTheObjectOfAGroupIsTheOneThatHasANameWhateverTheKeysOfTheOthersAndTheOrderOfTheRows
  - go:backend/internal/pgingest::TestIdnTheRecordsOfARunAreBoundToObjectsWhoseIdsAreTheOnesTheKeysGive
  - go:backend/internal/pgingest::TestIdnTheRecordsOfSmallUnitsWithReferencesNotSolvedAndWithEndsAreAllBound
  - go:backend/internal/pgingest::TestIdnTheSameChainWithTheLimitOfTheTurnsRaisedIsOneObjectAndMakesTheSameObjectInAnyOrderOfTheRows
  - go:backend/internal/pgingest::TestIdnTwoDatasetsThreeKindsOfKeyARepeatedKeyAndARecordThatIsClosedMakeTheObjectsTheOldPipelineMakes
  - go:backend/internal/pgingest::TestIdnTwoObjectsThatSayTheyHaveOneIdentifierGetItByTheRuleAndTheLoserIsToldAndAFeedThatMakesNoObjectFindsTheOnesThereAre
  - go:backend/internal/pgingest::TestIdnWhatTheTestsWatchOfTheStatementsOfAUnitIsOfTheStoreTheyAskItOfAndNotOfAnother
  - go:backend/internal/pgingest::TestIdnWhenEveryRecordIsInTheSamePlaceAUnitReadsOfTheRegistryWhatItDidWhenTheyWereInManyAndNoMore
  - go:backend/internal/pgingest::TestPgiACallThatNeedsATableThatIsDamagedFailsWithAnError
  - go:backend/internal/pgingest::TestPgiAKeyAnEndAndAClaimOfARowThatARuleFilteredOutAreHeldAndKeepTheNumberOfTheirFeed
  - go:backend/internal/pgingest::TestPgiAPageOfTenThousandRowsGoesInWithOneCopyOfEachTable
  - go:backend/internal/pgingest::TestPgiAPageThatFailsHalfWayLeavesNothingNotEvenItsPlaceAndNoRowIsHalfWritten
  - go:backend/internal/pgingest::TestPgiAPageWhoseRowOneTableRefusesLeavesNothingWhicheverTableItIsAndTheSinkGoesOn
  - go:backend/internal/pgingest::TestPgiARecordIsNewChangedOrTheSameAndOnlyNewAndChangedOnesCarryCellsKeysEndsAndClaims
  - go:backend/internal/pgingest::TestPgiARunMovesOnlyAlongTheStagesAndTheGateOfTheKGHoldsOneRunAtWork
  - go:backend/internal/pgingest::TestPgiASourceHasOneRunThatIsNotOver
  - go:backend/internal/pgingest::TestPgiASourcePutToAKGHasItsRowsAndItsPartitionsAndPuttingItAgainChangesNothingButTheDatasetsItAdds
  - go:backend/internal/pgingest::TestPgiAWriteOfAnAttemptThatIsNotTheRunsIsRefusedAndChangesNothing
  - go:backend/internal/pgingest::TestPgiAWriteOfAnOldAttemptAfterAnotherServerTookTheRunOverWritesNothing
  - go:backend/internal/pgingest::TestPgiDeletingAKGTakesItsSourcesOffFirstWithTheirRunsAndAKGWithNoSourcesIsDeletedAsBefore
  - go:backend/internal/pgingest::TestPgiEveryCallWithAContextThatHasEndedFailsAndChangesNothing
  - go:backend/internal/pgingest::TestPgiPuttingASourceToAKGAndTakingItOffHoldFewLocksAndDeletingAKGOfManySourcesFitsTheDefaultLockTable
  - go:backend/internal/pgingest::TestPgiReceiveStoresEveryPageWithItsPlaceAndMovesTheRunToComputing
  - go:backend/internal/pgingest::TestPgiReceivingAgainEmptiesTheSourcesPartitionAndBeginsAtTheFirstPageAndTheOtherSourcesAreLeftAlone
  - go:backend/internal/pgingest::TestPgiReceivingIsRefusedForAWrongPlanARunThatIsNotThereAndADatasetTheSourceWasNotPutWith
  - go:backend/internal/pgingest::TestPgiTakingASourceOffDropsItsPartitionsAndRowsInChunksAndRefusesASourceWithARunThatIsNotOver
  - go:backend/internal/pgingest::TestPgiThePlaceOfARecordIsItsRangeOnTheSheetElseItsLineAndTheLinesPassedOverAreCounted
  - go:backend/internal/pgingest::TestPgiTheProblemsOfARecordAreOnItsRowAndTheProblemsOfAPageOnTheRowMinusOneAndAnIncompleteDatasetIsToldToo
  - go:backend/internal/pgingest::TestPgiTheRowsOfAPageAreTheRowsThePreparerGaveWithTheCellsAndTheKeysIntact
  - go:backend/internal/pgingest::TestRdfABucketReadsTheIndexRangeItCoversAndNotTheWholeTableAndWhatItReadsDoesNotGrowWithTheSource
  - go:backend/internal/pgingest::TestRdfABucketThatFailsLeavesNothingAndThePlaceStaysAtIt
  - go:backend/internal/pgingest::TestRdfACallWithACanceledContextIsRefusedByEveryStepOfTheComparison
  - go:backend/internal/pgingest::TestRdfADatasetReadAsWhatWasSeenCountsNoAbsenceAndADatasetLeftOutWholeCountsNothingAndADatasetThatNeverRetiresWaitsForNothing
  - go:backend/internal/pgingest::TestRdfADatasetTheReceivingDidNotGiveIsLeftOutWhole
  - go:backend/internal/pgingest::TestRdfAKeyHasOneReasonByOrderOfWeightSoWhatIsLetGoOfIsToldByTheWaitAndWhatIsShownAgainByTheCounts
  - go:backend/internal/pgingest::TestRdfAKeyThatRepeatsKeepsItsProblemFromItsStartThroughTheRunsThatDoNotChangeItToItsEnd
  - go:backend/internal/pgingest::TestRdfARowWhoseHashIsTheSameAndWhoseFilteringTurnsOverMakesTheObjectsTheOldPipelineMakes
  - go:backend/internal/pgingest::TestRdfARunThatFindsTheTablesOfTheComputationAnotherRunsHasNotBegunAndDoesTheComparisonAgainFromItsStart
  - go:backend/internal/pgingest::TestRdfAServerThatTakesTheRunOverGoesOnFromTheBucketTheRunIsAtAndCountsAtTheTimeTheComparisonBeganAt
  - go:backend/internal/pgingest::TestRdfAStepThatTheDatabaseRefusesIsToldAndTheBucketLeavesNothing
  - go:backend/internal/pgingest::TestRdfAWordOfCoverageThatIsNotOneOfThreeIsReadAsIncompleteAndALineWithNoWordIsReadByItsCompleteFlag
  - go:backend/internal/pgingest::TestRdfEveryFateOfAKeyIsToldAndTheWaitsAreCountedAtTheTimeOfTheRun
  - go:backend/internal/pgingest::TestRdfTheBucketsAreRangesOfDatasetAndKeyThatFollowOneAnotherAndTheLastHasNoEnd
  - go:backend/internal/pgingest::TestRdfTheComparisonDoesNotBeginForARunThatIsNotComputingOrForAnotherAttempt
  - go:backend/internal/pgingest::TestRdfTheComparisonGivesTheRawRecordsTheWaitsTheCountsAndTheLiveObjectsTheOldPipelineGivesForTheSameCaptures
  - go:backend/internal/pgingest::TestRdfTheComparisonGivesWhatTheOldPipelineGivesForRandomCapturesAndAnyCutOfTheBuckets
  - go:backend/internal/pgingest::TestRdfTheComparisonReadsTheCoverageOfTheReceivingFromTheCountsAndTakesTheOldFlagWhereThereIsNone
  - go:backend/internal/pgingest::TestRdfTheCountsOfTheRunSayForEachDatasetHowMuchOfItTheNextStepMayCount
  - go:backend/internal/pgingest::TestRdfWhatIsMadeDoesNotDependOnHowTheWorkIsCutIntoBuckets
  - go:backend/internal/pgingest::TestRdfWhatTheComparisonMadeOfARunIsWhatTheNextRunStartsFrom
test_change_reason: 생성기 버전이 7에서 8이 되었다. 테스트는 단언을 한 글자도 약하게 하지 않고 버전 숫자만 맞춘다(버전 8이 더하는 단계는 그 버전의 테스트에 있다). pgingest 테스트들(Exp, Hka, Idn, Pgi, Rdf 89개)은 본문이 그대로이고 지문만 바뀌었다. 모두가 부르는 도우미 `pgiOpen`이 KG를 테스트 전용 모델 `pgstore.TestOnlyModelsWithoutQuerySchema`로 만든다. 이 모델은 서버의 모델(`StandardModels(true)`)과 같고 질의 스키마만 없다. 통계 객체는 서버처럼 함께 만들므로 실행 계획과 읽은 행을 보는 테스트(`idn_plan_test.go`, `rdf_plan_test.go`, `exp_scale_test.go`)는 서버와 같은 조건에서 계획을 본다. 이 테스트들이 보는 것은 수집이고 질의 스키마는 `Qsc` 테스트가 덮는다. 도우미는 모델을 얻다 실패하면 `t.Fatal`로 끝나는 검사 하나를 더했을 뿐이고 단언과 리터럴은 하나도 바뀌지 않았다.
decision_hash: 0315e0d9de88e997
---
# ADR-0113 질의 스키마 — 등급마다의 스키마와 역할, 뷰와 함수, 등급을 거르는 한 곳

## 맥락

ADR-0080이 사용자가 SQL로 KG를 묻는 질의 스키마를 정했고, ADR-0085와 ADR-0087이 저장 스키마의 계획과 실행기를 지었다. 질의 스키마(타입 뷰, 링크 뷰, 공통 뷰, 과거 시점 함수, 모델 설명, 등급마다의 스키마와 역할)는 아직 없었다. 이 변경이 그것을 짓는다. 질의 실행기(역할로 바꾸기, `search_path` 걸기), 그래프 함수, REST와 MCP는 뒤의 변경이다.

## 결정

1. **KG마다 질의 스키마 여섯 개**: 등급(`open`, `restricted`, `confidential`) 셋 곱하기 일반과 큰 적용 중용(`_w`) 둘이다. 이름은 `q_<kg_no>_<등급>[_w]`다. 일반 세트에서 높은 등급의 스키마는 낮은 등급과 정의 글이 다른 객체만 가진다. `_w` 세트는 등급마다 완전한 한 벌이다(큰 적용 중에만 쓰는 것이라 단순한 쪽을 고른다). `search_path`는 높은 것부터 겹친다(`pgschema.SearchPath`).
2. **등급을 거르는 한 곳**: 속성의 민감도를 읽는 코드는 `pgschema/qgrade.go`(`attrGrades`, `VisibleAttrNos`) 한 곳이다. 모든 뷰, 함수, 설명, 변경 기록은 여기서 나온 속성 번호 집합으로 거른다. 소스를 훑는 테스트가 다른 파일이 민감도를 읽지 않음을 잠그고, 권한 테스트가 `provenance`, `attribute`, `object_detail`, `<타입>_at`, `value_change`, `model_attribute`가 같은 집합으로 걸러짐을 확인한다. 같은 번호를 두 속성이 쓰면 더 민감한 쪽을 따르고, 민감도를 알 수 없는 값은 모든 등급에서 숨긴다(`noteGrade`, `gradeRank`).
   **값을 지키는 것은 문서를 펴기 전에 숨길 키를 빼는 것이다.** 문서를 통째로 펴는 문장(`attribute` 뷰의 `jsonb_each`와 `object_detail`의 `jsonb_each`, 일반 세트와 `_w` 세트 모두)은 `doc - ARRAY['11', '12']::text[]`를 편다(`gradeView.withoutHidden`, 같은 파일). 행 조건(`ma.attr_no <> ALL (…)`, `notHidden`)은 둘째 울타리이고 보장이 아니다. 뷰는 `security_barrier`가 아니라서 계획기가 호출자의 조건, 형 변환 오류, 비용이 아주 낮은 함수를 뷰의 행 조건보다 먼저 돌릴 수 있고, 그러면 숨은 값이 오류 글이나 함수가 본 값이나 0 나누기의 유무로 샌다(독립 검토가 `SELECT` 한 줄로 재현했다). 번호 하나를 정해 읽는 곳(타입 뷰, `<타입>_at`, `provenance`, `link`)은 보이는 번호만 식에 적으므로 숨은 키를 열지 않는다. `security_barrier`는 쓰지 않는다(성능 때문에 기각했다, [질의 스키마](../query-schema.md) 8절). 테스트는 비슈퍼유저 등급 역할로, 슈퍼유저가 만들어 `EXECUTE`를 준 비용 낮은 함수가 모든 면에서 본 값을 모아 숨은 값이 없음을 보고, 형 변환 오류나 0 나누기로 값을 알아내는 조건(오라클)이 숨은 값에서 실패하지 않음을 보고, 같은 조건이 그 값을 보는 등급에서는 실패함(오라클이 제대로 동작함)을 본다.
3. **역할** `pq_<설치 토큰>_<kg_no>_<등급>`: `NOLOGIN NOINHERIT`이고 저장 스키마와 다른 KG의 스키마에 권한이 없다. 자기 등급과 낮은 등급의 질의 스키마(일반과 `_w`)의 `USAGE`, 뷰의 `SELECT`, 함수의 `EXECUTE`만 가진다. 설치 토큰은 DB마다 한 번 정해지는 12자리 16진수이고 새 공용 마이그레이션 `0008_install_token.sql`이 둔다(역할은 클러스터에 속하므로 DB 둘이 같은 이름을 만들지 않게 하려는 것이다, Q2). 토큰은 이름에 들어가므로 계획이 모양을 검사한다(`CheckInstallToken`). **실행 계정은 모든 등급 역할의 멤버**라서(아래 4항) 등급 역할로 도는 문장이 `SET ROLE`이나 `RESET ROLE`로 다른 등급의 역할이나 실행 계정으로 오갈 수 있다(독립 검토가 재현했다). 그래서 등급의 분리는 역할만으로 서지 않고 질의 실행기가 그런 문장을 통과시키지 않는 데 선다(11항).
4. **PostgreSQL 18의 사실**(확인했다): (가) `CREATEROLE`로 만든 역할은 만든 쪽이 `SET ROLE` 하지 못한다(만든 쪽에는 admin만 주어진다). 실행기가 역할마다 `GRANT <역할> TO CURRENT_USER WITH SET TRUE, INHERIT FALSE`를 한다. 설치 계정은 슈퍼유저가 아니라 DB 소유자에 `CREATEROLE`이면 되고 테스트가 그 계정으로 돈다(Q2). (나) 비슈퍼유저의 `REVOKE EXECUTE … FROM PUBLIC`은 내장 함수에 효과가 없다(경고만 난다, Q1). 그래서 위험한 내장 함수의 실행 권한을 회수하는 설치 단계는 이 변경이 짓지 않고 질의 실행기의 변경이 정한다.
5. **뷰와 함수**: 타입마다 뷰(이름은 타입 이름의 snake_case, 부모는 자기 표와 모든 하위 타입 표의 `UNION ALL`, D-8: 객체는 구체 타입의 표에만 있어서 한 객체가 한 번만 나온다), 링크 타입마다 뷰, 공통 뷰(`object`, `link`, `attribute`, `identifier`, `kg_version`, `change`, `value_change`), 모델 설명 뷰 넷, 함수 `object_detail(id)`(등급마다 하나), `<타입>_at(변경 번호)`. 모든 뷰와 컬럼과 함수에 `COMMENT`가 있다. 근거 등급은 글자(`fact`, `assumed`, `unknown`)로 보인다. `provenance`는 이름이 키이고 `g`, `b`, `t`, `r`이 안의 키이며 값은 넣지 않는다. 변동 속성은 `<이름>`과 `<이름>_at` 두 컬럼이다. 속성 이름이 `name`이면 컬럼을 따로 두지 않는다. 컬럼 이름이 겹치거나 63바이트를 넘거나 예약된 이름과 같으면 계획이 거절한다. 조직 속성은 이름 그대로 큰따옴표로 감싼 컬럼이다.
6. **함수의 권한**: 함수는 `SECURITY DEFINER`에 `search_path = pg_catalog, pg_temp`로 고정하고 `PUBLIC`의 실행 권한을 회수한다(질의 역할은 저장 스키마를 못 읽으므로 함수가 소유자의 권한으로 읽는다). 등급은 함수 안에 들어 있고 호출자가 올릴 수 없다. 뷰 안에서 부르는 시각 변환 함수 `public.praxium_ts`만 `PUBLIC`이 실행한다.
7. **`<타입>_at(n)`**: 변동 값과 상태가 없는 같은 컬럼을 낸다. `n`이 이력의 바닥보다 작거나 읽기 워터마크보다 크거나 `NULL`이면 빈 결과가 아니라 SQLSTATE `PXNRD`의 오류다(온전해 보이지만 온전하지 않은 답을 막는다).
8. **모델 설명 표 넷**(`model_type`, `model_attribute`, `model_link_type`, `model_link_rule`)은 저장 스키마의 고정 표이고 생성기 버전 8의 더하는 단계다(이미 있는 표에도 되풀이할 수 있게 `IF NOT EXISTS`다). 질의 스키마를 만드는 트랜잭션이 표를 비우고 다시 채운다.
9. **다시 다시 만들기와 삭제**: 스키마 관리기가 일을 할 때마다 질의 스키마 여섯 개를 지우고 통째로 다시 만든다(만든 것과 따라간 것이 같다, 테이블은 건드리지 않는다). KG를 지우면(`DeleteKG`, `DropStorageSchema`) 질의 스키마와 역할도 같은 트랜잭션에서 지운다.
10. **생성기 버전 8**: 두 커밋(B3-1과 B3-2)이 한 버전 안이다. 버전 7은 먼저 합쳐진 변경(ADR-0108)이 썼으므로 이 변경이 8로 올렸다. 이미 있는 KG의 질의 스키마는 이 버전에 묶여 다시 만들어진다(시작할 때마다가 아니다): 서버가 시작할 때 생성기가 옛것인 KG를 따라가고(`AdjustOldSchemas`), 스키마 관리기가 일을 할 때(모델이 바뀌어 더하는 변경) 지우고 다시 만든다. 뷰의 SQL만 바뀌고 버전이 같으면 이미 있는 KG는 다시 만들어지지 않으므로, 합친 뒤에 뷰의 SQL을 바꾸는 변경은 생성기 버전을 올려야 한다. 근거(코드 위치): 생성기 버전은 `pgschema/plan.go:22`의 상수이고, 서버 시작은 `pgstore/schema_startup.go:90`(`needsAdjusting`)이 기록의 버전이 상수보다 낮은 KG를 골라 따라가며, 따라갈 일이 없으면(`schema_adjust.go:81`, 기록과 모델이 같으면) 질의 스키마를 건드리지 않고, 있으면 `schema_adjust.go:87`(`runQuerySteps`)이 여섯 스키마를 지우고 다시 만든다. 이 PR의 `attribute`와 `object_detail`의 SQL 변경은 이 PR 안에서 생성기 버전 8이 처음 나가는 것이므로 올릴 필요가 없다(버전 8이 main에 들어가기 전이다).
11. **등급 밖의 면과 모델 검증**: 등급 필터링 밖에서 보이는 것은 객체의 이름(`name` 열), 참조(`ref`), `min_grade`, `change` 뷰의 변경 표시(숨은 속성만 바뀐 변경도 `changed`로 보인다), 모델 설명(`model_attribute`의 이름, 설명, 열거값과 `value_change`의 속성 이름)이다. 값이 아니라 메타데이터라서 이번에는 거르지 않고, **모델 설명은 등급 밖의 메타데이터**라고 정한다(숨은 속성의 이름, 설명, 열거값이 비용 순서에 따라 호출자의 함수에 보일 수 있다). 값이 새는 길 둘은 모델 검증이 막는다. (가) 객체의 이름을 주는 속성 `name`(`model.ObjectNameProperty`)은 표의 `name` 열에서 모든 등급에 보이므로 민감도가 `open`이 아니면 거절한다. (나) 식별 키의 속성은 `open`이어야 한다: 수집이 키의 첫 성분의 값을 키의 식별자 종류로 기록하고(`ingest/derive.go`의 `claim`) `identifier` 뷰는 등급이 없어서, `restricted` 속성이 키이면 그 값이 `open` 역할에 보인다. 식별 키는 `confidential`만 안 된다던 ADR-0018 10항을 넓힌 것이다(`amends`).
12. **질의 실행기의 필수 조건**(REQ-666, B3-4가 테스트로 잠근다): 질의 실행기는 `SET`, `RESET`, `SET ROLE`, 둘 이상의 문장, `DO`, `CREATE`(임시 객체 포함)를 거절하고, 스키마 이름을 적은 질의는 그 등급의 지금 세트(일반 또는 `_w`)의 것만 통과시킨다. 앞의 것은 3항의 멤버십 때문에 없으면 등급이 무너지는 것이고, 뒤의 것은 큰 적용 중에 일반 세트를 이름으로 골라 온전해 보이지만 온전하지 않은 답(REQ-627)을 받는 길을 막는 것이다. 등급 역할이 `TEMP` 권한을 가져 임시 표와 임시 함수를 만들 수 있다는 것도 같은 조건으로 막는다. 이 변경에서는 코드가 아니라 조건을 적는다.

## 이유

- 등급을 거르는 한 곳이 없으면 뷰, 함수, 변경 기록 가운데 하나가 높은 등급의 값을 새게 하기 쉽다. 한 곳과 그것을 잠그는 테스트가 값이 새는 길을 구조로 막는다.
- 역할을 클러스터가 아니라 DB에 속하게 만들 수 없으므로 이름의 토큰이 충돌을 막는다.
- 함수는 저장 스키마를 읽어야 하는데 질의 역할은 그것을 못 읽으므로 `SECURITY DEFINER`가 필요하고, 그래서 `search_path` 고정과 `PUBLIC` 회수가 함께 필요하다.

## 대안과 기각 사유

- 등급마다 같은 뷰를 다 두기: 뷰가 세 배가 된다. 일반 세트는 다른 것만 둔다.
- `object_detail`에 등급 인자: 호출자가 올릴 수 있는 인자가 되어 스키마의 사용 권한에 기대는 깨지기 쉬운 구조가 된다.
- 읽을 수 없는 변경 번호에 빈 결과: 온전해 보이지만 온전하지 않은 답이 된다.
- `security_barrier` 뷰로 값을 지키기: 숨은 값이 호출자의 조건보다 먼저 돌지 않게 하지만 조건이 인덱스로 내려가지 않아 수십 배 느려진다([질의 스키마](../query-schema.md) 8절). 문서를 펴기 전에 키를 빼면 같은 보장을 비용 없이 얻는다.
- 등급마다 `LOGIN` 역할로 따로 연결하기(실행 계정이 등급 역할의 멤버인 관계 자체를 없앤다): `SET ROLE`로 오가는 길이 사라지지만 연결 풀이 등급 수만큼 늘고 비밀번호나 인증서를 역할마다 두어야 한다. 이번에는 고르지 않고 실행기가 문장을 거르는 쪽(12항)을 정했다. 실행기가 걸러도 새는 것이 나오면 다시 본다.
- 숨은 속성의 모델 설명 행도 펴기 전에 빼기: `model_attribute`와 `value_change`는 값이 아니라 이름과 설명이라서 이번에는 하지 않고 등급 밖의 메타데이터로 둔다(11항).

## 결과

- 이름만 바뀐 모델은 모델 해시가 같아(이름은 해시에 들지 않는다) 스키마 관리기가 일을 하지 않으므로 뷰가 다시 만들어지지 않는다. 다른 변경과 함께 관리기가 돌 때는 모든 뷰가 새 이름으로 다시 만들어진다. 이름만 바뀐 경우에 다시 만드는 길은 아직 없고 [질의 스키마](../query-schema.md) 12절의 미결이다.
- 표준 모델의 KG 하나를 만드는 시간이 약 1.6초에서 약 2.7초로 는다(질의 스키마의 836문장, 측정 1.66초: 뷰 0.89초, 권한 0.38초, 함수 0.19초이고, 이미 있는 위에 다시 만들면 1.43초. 같은 기계에서도 부하에 따라 1.4~1.7초로 달라진다).
- 질의 스키마를 쓰는 처리 경로(질의 실행기, `SET LOCAL ROLE`, `search_path`, 그래프 함수, REST와 MCP)은 뒤의 변경이다.
- 테스트의 시간: 표준 모델의 KG 하나를 만들 때마다 질의 스키마가 약 2초를 더하므로, KG를 여러 번 만드는 일반 모음은 질의 스키마 없는 모델로 돈다. 두 갈래다: pgstore의 일반 모음(`NewTestStore`)은 원래 쓰던 `StandardModels(false)`(통계 객체도 없음)에서 질의 스키마도 빠지고, 다른 패키지의 테스트(pgingest의 `pgiOpen`)는 서버의 모델에서 질의 스키마만 뺀 `TestOnlyModelsWithoutQuerySchema`를 쓴다. 질의 스키마를 실제로 만드는 길(`CreateStorageSchema`, `CreateKG`, 조정, 삭제, 서버 시작)은 `Qsc` 테스트가 각각 덮고, 서버의 어떤 파일도 그 모델을 만들지 않음(`StandardModels(false)`나 `TestOnlyModelsWithoutQuerySchema`를 부르거나 `noQuery`를 쓰지 않음)을 소스를 훑는 테스트가 잠근다.

## 미결 사항

- **같은 클러스터에 복사한 DB**는 설치 토큰이 같아 역할을 공유하고, 한쪽의 KG 삭제가 `2BP01`(다른 DB의 객체가 그 역할에 의존)로 실패한다. 베타 전에 고친다(토큰이 이 DB의 것인지 확인하는 표지, 또는 있는 역할이 이 설치의 것인지 확인).
- **`TEMP` 권한**: 등급 역할은 DB의 `TEMP` 권한을 `PUBLIC`에서 받아 임시 표와 임시 함수를 만들 수 있다. 실행기가 `CREATE`를 거절하는 것(12항)으로 닫을지, 설치 단계에서 `REVOKE TEMPORARY ON DATABASE … FROM PUBLIC`을 할지(필요한 계정에만 `GRANT`)는 질의 실행기의 변경이 정한다. 이 변경의 테스트는 `TEMP` 권한에 기대지 않는다.
- 숨은 속성의 이름, 설명, 열거값이 모델 설명과 변경 기록으로 새는 것(11항)은 메타데이터로 받아들였다. 거를 필요가 생기면 `model_attribute`와 `value_change`의 행 조건 앞에서 뺀다.
