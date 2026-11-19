---
title: ADR-0096 옛 이름의 별칭 파일을 지우고 저장을 여는 길을 `Open` 하나로 한다
kind: decision
status: active
enforced_by:
  - TLOCK001
  - backend/internal/pgstore/kgstore_default_model_test.go
  - backend/internal/pgstore/kgstore_extra_test.go
touches_protected:
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
amends_tests:
  - go:backend/internal/pgcheck::TestPgxIngestionGivesTheSameGraphHashAsMemory
  - go:backend/internal/pgcheck::TestPgxQueriesAnswerTheSameAsMemory
  - go:backend/internal/pgstore::TestRsmALeaseIsGivenAfterTheAbandonThatWasCutIsEnded
  - go:backend/internal/pgstore::TestRsmALeaseIsGivenAfterTheRestoreThatWasCutIsEndedAndTheKGIsOpenAgain
  - go:backend/internal/pgstore::TestRsmALeaseOfAKGWithNoWorkHalfDoneIsGivenAtOnce
  - go:backend/internal/pgstore::TestRsmTheServerEndsTheWorkOfEveryKGAndOneThatCannotBeEndedDoesNotStopTheOthers
  - go:backend/internal/pgstore::TestV2StorePassesThePreviewSuite
  - go:backend/internal/pgstore::TestV2StorePassesTheSuite
  - go:backend/internal/pgstore::TestV2cGuardChecksInOrderAndTheHeadRoundTrips
  - go:backend/internal/pgstore::TestV2cLeaseIsTakenRenewedAndReleasedByTheDatabaseClock
  - go:backend/internal/pgstore::TestV2cLeaseRunsOutByTheClockOfTheCallerWhenTheStoreIsAskedTo
  - go:backend/internal/pgstore::TestV2cResolveDecidesAnUnknownCommitByTheHead
  - go:backend/internal/pgstore::TestV2dEveryOperationFailsOnAClosedPool
  - go:backend/internal/pgstore::TestV2dReadsFailOnRowsThatHoldWhatTheStoreDidNotWrite
  - go:backend/internal/pgstore::TestV2dReadsFailWhenATableTheyNeedIsMissing
  - go:backend/internal/pgstore::TestV2eALeaseAndAHeadThatAreDamagedFail
  - go:backend/internal/pgstore::TestV2eAServerThatHoldsAnotherModelRefusesTheKG
  - go:backend/internal/pgstore::TestV2eReadPointsAndVersions
  - go:backend/internal/pgstore::TestV2eTheCatalogRefusesDamagedRowsAndAnUnknownModel
  - go:backend/internal/pgstore::TestV2eTheTestStoreMakesKGsAndTheCatalogFindsThemByIdNameAndMain
  - go:backend/internal/pgstore::TestV2mAChunkWithAValueThatHasNoPlaceIsRefused
  - go:backend/internal/pgstore::TestV2mASealThatIsNotReadyOrDoesNotFollowIsRefused
  - go:backend/internal/pgstore::TestV2mAnUnknownOutcomeOfTheStartOfAWorkIsDecidedByTheHead
  - go:backend/internal/pgstore::TestV2mWritesAndFeedsFailWhenATableIsGone
  - go:backend/internal/pgstore::TestV2oAnApplyWhoseCommitGotNoAnswerIsCheckedAgainstTheHeadAndCanBeRepeated
  - go:backend/internal/pgstore::TestV2rCatalogListsGetsAndDeletesAKG
  - go:backend/internal/pgstore::TestV2rReadsLinksIdentifiersAndVolatileValues
  - go:backend/internal/pgstore::TestV2rReadsTheNewestAndThePastStates
  - go:backend/internal/pgstore::TestV2rRefusesReadsThatCannotBeAnswered
  - go:backend/internal/pgstore::TestV2wAChangeOfValuesAloneDoesNotTouchTheRowsOfTheObjectAndTheRowsAreWrittenInOrder
  - go:backend/internal/pgstore::TestV2wARefThatCameBackWithAnotherTypeIsRefused
  - go:backend/internal/pgstore::TestV2wARetiredObjectComesBackWithTheSameNumberAndTheKeyStaysBound
  - go:backend/internal/pgstore::TestV2wAWriteIsRefusedWhenTheSchemaWasMadeFromAnotherModel
  - go:backend/internal/pgstore::TestV2wAbandonTakesTheChunksOutOneNumberAtATimeAndLeavesNoTraceAndAnInterruptedOneIsEnded
  - go:backend/internal/pgstore::TestV2wBulkReadsGiveWhatReadsOneAtATimeGive
  - go:backend/internal/pgstore::TestV2wRestoreClosesTheKGUntilItEndsAndAnInterruptedOneIsResumed
  - go:backend/internal/pgstore::TestV2wTheSettingsOfTheTablesAreInForce
test_change_reason: ADR-0094가 남긴 옛 이름 별칭 파일을 지운다. 잠긴 테스트가 부르는 이름만 바뀌고 단언, 기대 값, 흐름은 그대로이며, 모델 필수 검사 단언 한 개는 기본 모델 테스트로 대체된다(0096 본문 참고)
decision_hash: 0a9252f38263ecc6
---
# ADR-0096 옛 이름의 별칭 파일을 지우고 저장을 여는 길을 `Open` 하나로 한다

## 맥락

ADR-0094가 옛 저장을 지우고 새 저장이 그 자리를 대신하게 했다. 그때 잠긴 테스트가 옛 이름(`StoreV2`, `OpenV2`, `V2Options`, `NewTestStoreV2`, `NewTestStoreV2With`)을 계속 불렀으므로, 그 이름들을 제품의 이름에 대는 별칭으로 남겼다. 저장은 하나뿐이므로 이 이름들은 뜻이 없고, 별칭 파일은 옛 저장의 흔적이다.

## 결정

별칭 파일을 지운다. 잠긴 테스트가 부르는 식별자를 제품의 이름으로 바꾼다. 테스트 함수의 이름, 단언, 기대 값, 흐름은 바꾸지 않는다.

| 옛 이름 | 새 이름 | 비고 |
|---|---|---|
| `StoreV2` | `Store` | 타입 |
| `NewTestStoreV2With(tb, clock)` | `NewTestStoreWith(tb, clock)` | |
| `NewTestStoreV2(tb)` | `NewTestStoreWith(tb, LeaseClockDB)` | 옛 이름은 데이터베이스의 시계를 썼고 `NewTestStore`는 호출자의 시계를 쓰므로, `NewTestStore`로 바꾸면 행동이 바뀐다 |
| `OpenV2(ctx, cfg, V2Options{Models, LeaseClock})` | `Open(ctx, Config{..., Models, LeaseClock})` | `V2Options`는 없어진다 |

테스트 파일에서 `StoreV2`, `OpenV2`, `NewTestStoreV2`를 부르던 곳은 모두 이 표의 새 이름으로 바꿨다. 주석과 `ARCHITECTURE.md`의 옛 이름 문장도 사실에 맞게 고쳤다.

## 단언 하나가 사라진다

`OpenV2`는 모델을 주지 않으면 `CodeInvalidValue`로 거절했다(사례 "no models"). `Open`은 모델이 nil이면 표준 모델(통계 객체 포함)을 쓰는 것이 정해진 동작이고(ADR-0094), 별칭 파일과 함께 이 거절도 사라진다. 따라서 "no models" 사례(호출과 그 단언 한 줄)를 지운다. 같은 테스트의 다른 사례와 단언은 그대로다.

- 사라지는 단언: 별칭 함수의 모델 필수 검사(`TestV2eTheTestStoreMakesKGsAndTheCatalogFindsThemByIdNameAndMain`의 "no models").
- 대신 지키는 테스트: `TestV2sOpenWithoutModelsHoldsTheStandardModelWithItsStatistics`(새 파일 `kgstore_default_model_test.go`). `Open`에 모델을 주지 않으면 표준 모델과 통계 객체를 가진 모델이 열린다는 것을 확인한다.

## 이유

- 별칭은 제품의 이름을 옛 이름으로 한 번 더 노출한다. 지우지 않으면 저장을 여는 길이 둘로 보인다.
- 잠금 대상 테스트는 식별자 이름을 이름으로 잠그지 않는다. 이름만 바꾸면 단언과 기대 값은 그대로이고, 바뀌는 것은 지문뿐이다. 그래서 이 변경은 ADR로 건별 기록한다.

## 대안과 기각 사유

- 별칭 파일을 남긴다: 옛 이름이 계속 제품 코드의 일부로 남는다. 기각.
- `Open`이 모델 필수 검사를 한다: `Open`의 기본 모델 동작을 바꾸는 것이다. 설계 책임자가 가지 않기로 했다. 기각.
- 모델 필수 검사 단언을 그냥 지우고 대체 테스트를 두지 않는다: 기본 모델의 행동이 아무도 확인하지 않게 된다. 기각.

## 결과

- 저장을 여는 길은 `Open` 하나다. `v2names.go`는 없다.
- 잠금 파일의 37개 키가 바뀐다(`amends_tests`). 그중 `asserts`와 `literals`가 바뀌는 것은 "no models" 단언을 지운 테스트 하나뿐이다.
- 새 테스트 하나가 잠긴다.
