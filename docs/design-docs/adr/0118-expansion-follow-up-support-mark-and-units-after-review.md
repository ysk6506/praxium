---
title: ADR-0118 확장과 퇴역 후보의 검토 뒤 고침 — 지지의 범위, 영구 표지, 식별이 남긴 짝, 전체 계산의 범위, 한 번만 읽는 키
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOARCH010
  - backend/internal/ingest/exp_cards_test.go
  - backend/internal/pgingest/exp_followup_test.go
  - backend/internal/pgingest/exp_scale_test.go
  - backend/internal/pgschema/exp_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0114-expansion-of-the-affected-set-and-retire-candidates.md
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0114"
    what: 결정 3(퇴역 후보의 조건 2와 3, 표의 "다른 소스의 레코드가 서 있는 객체" 행), 결정 5(스키마를 바꾸지 않는다, 생성기 7), 결정 6(읽는 양, 단위 안에서만 한 번)과 알려진 한계(단위를 넘어 같은 키를 다시 읽는다, 진 링크 문구), 결정 1의 `ExpandStart`
amends_tests:
  - go:backend/internal/pgingest::TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords
  - go:backend/internal/pgingest::TestExpAStatementThatFailsInAUnitLeavesTheUnitUndoneAndTheRunGoesOnFromIt
  - go:backend/internal/pgingest::TestExpTheExpansionDoesNotBeginOnARunWhoseIdentificationIsNotDoneAndTheComputationOfEverythingGoesByUnitsOfRecords
  - go:backend/internal/pgingest::TestExpTheExpansionOfARunOfAWorldThatHasACaseForEveryRule
  - go:backend/internal/pgingest::TestExpTheSetIsTheSameForAnyOrderOfTheRowsReceivedAndOfTheNumbersOfTheRecords
  - go:backend/internal/pgingest::TestExpAnObjectIsOnlyACandidateForRetirementWhenAFeedThatRetiresMadeIt
  - go:backend/internal/pgingest::TestExpAnObjectThatARecordOfAnotherSourceStandsUnderIsNotACandidateAndOneThatNoRecordStandsUnderIs
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgschema::TestRcpDiffFromGeneratorSevenToEightAddsTheTwoIndexesOfTheComputationOfTheObjectsAndNothingElse
  - go:backend/internal/pgingest::TestRcpTheComputationIsRefusedBeforeTheExpansionIsDoneAndAPageThatIsNotTheNextOneIsRefused
  - go:backend/internal/pgingest::TestRcpAStatementThatFailsLeavesTheRunWhereItWasAndWhatIsWrittenIsNotKept
  - go:backend/internal/pgingest::TestRcpTheObjectsTheRunTouchedAreTheSameWhenEveryRecordIsComputedAndTheOthersAreWhatTheRunBeforeMade
  - go:backend/internal/pgingest::TestRcpTheClaimRuleOfTheExpansionIsNeededForTheObjectsThatHoldAClaimToBeComputed
  - go:backend/internal/pgingest::TestRcpARunStoppedBetweenPagesGoesOnFromTheNextPageAndStagesWhatOneThatDidNotStopStages
  - go:backend/internal/pgingest::TestRcpTheReportIsWhatTheStatementsWroteAndAnObjectWithoutRecordsOrWithOnesThatCannotBeReadIsLeft
  - go:backend/internal/pgingest::TestRcpACellAFeedThatMakesNoObjectCannotReadIsAProblemOfTheRecordAndTheValueIsLeftOut
  - go:backend/internal/pgingest::TestRcpAVolatileValueIsStagedAsAnEntryAndLeftOutOfTheDocument
  - go:backend/internal/pgingest::TestRcpTwoRecordsOfOneObjectThatSayDifferentThingsHaveOneValueAndTheLosingOneIsAProblem
  - go:backend/internal/pgingest::TestRcpAPageIsDoneOnceAndAServerThatLostTheLeaseIsRefusedEvenToRead
  - go:backend/internal/pgingest::TestRcpThePageSizeDoesNotChangeWhatIsStaged
  - go:backend/internal/pgingest::TestRcpTheStagedObjectsAreTheOnesTheOldPipelineMakesFromTheSameCaptures
test_change_reason: 단언을 지우거나 임계값을 넓힌 것은 하나도 없다. TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords는 단언이 강해졌다 — 옛 테스트는 한 크기(단위 10m, 각 문장이 한 단위에서 읽은 가장 큰 값)로 일부 문장만 쟀는데, 새 테스트는 m보다 작은 단위(50)와 한 단위(10000)를 모두 하고 모든 단위가 읽은 행을 합해서 읽는 여덟 문장 모두의 m(100)과 3m(300)을 재며, 3.4배 이하 단언과 m 이상 단언은 그대로다(F-5, 결정 5). 세상에 문장을 재려고 레코드 그룹을 더해서(지금 주는 끝 키, 퇴역 객체에서 나가고 들어오는 링크) 센 수의 기대가 바뀌었다 — KeyRecords와 LimitRecords는 m에서 2m, RetireRecords(2m)와 OldObjects(3)는 새 단언이고, ClaimObjects(m)와 Retiring(1)은 그대로다. 같은 센 수를 두 크기 모두에서 단언한다. TestExpAStatementThatFailsInAUnitLeavesTheUnitUndoneAndTheRunGoesOnFromIt와 TestExpTheExpansionDoesNotBeginOnARunWhoseIdentificationIsNotDoneAndTheComputationOfEverythingGoesByUnitsOfRecords는 확장의 시작에 피드 표(`Feeds`)를 더해야 해서 시작 값만 바뀌었고 단언은 그대로다(결정 1, 4). TestExpTheExpansionOfARunOfAWorldThatHasACaseForEveryRule, TestExpTheSetIsTheSameForAnyOrderOfTheRowsReceivedAndOfTheNumbersOfTheRecords, TestExpAnObjectIsOnlyACandidateForRetirementWhenAFeedThatRetiresMadeIt, TestExpAnObjectThatARecordOfAnotherSourceStandsUnderIsNotACandidateAndOneThatNoRecordStandsUnderIs는 도우미(`expRun`의 `Feeds`, `expReset`의 `calc_xkey` 비우기, `expMakeWorld`가 테스트가 넣는 다섯 레코드 X1에서 X5를 데이터셋의 지금 선언 개정으로 맞추는 것)가 바뀌어 지문만 바뀌었고 단언과 입력의 뜻은 그대로다. 마지막 것은 전체 계산이 식별이 읽는 레코드만 취하도록 바뀌어서(결정 4) 선언 개정이 다른 채로 넣은 테스트용 레코드가 빠지지 않게 한 것이다. TestControlReservesThePartitionPrefix는 생성기 버전의 기대를 8에서 9로 바꿨다(결정 3, 5). TestRcpDiffFromGeneratorSevenToEightAddsTheTwoIndexesOfTheComputationOfTheObjectsAndNothingElse는 이 빌드의 세대를 단언하던 줄을 지우고 세대 8을 정해 놓은 뒤 7에서 올리는 Diff를 본다. 이 빌드의 세대가 9라서 옛 단언이 깨지므로 뜻(7에서 8로 가는 Diff는 두 색인만 더한다)과 나머지 단언(단계의 종류, 트랜잭션 안, `IF NOT EXISTS`, `to_regclass`, 순서)은 그대로다. TestRcpTheComputationIsRefusedBeforeTheExpansionIsDoneAndAPageThatIsNotTheNextOneIsRefused는 확장 진행 중 상태를 써 넣을 때 실제 BeginExpand가 늘 쓰는 단위 크기(`DefaultExpandRows`)를 함께 넣었다. 이 PR이 claim 규칙의 단위 끝을 SQL OFFSET으로 찾게 바뀌어 크기 없는 상태(실제로는 생기지 않음)를 읽을 수 없게 됐다. 단언 그대로다. 나머지 rcp 시험들은 rcp 도우미(`rcpExpand`)가 확장 시작에 피드 표(`expFeeds`)를 더해서 지문만 바뀌었고 단언과 입력의 뜻은 그대로다(결정 1, 4).
decision_hash: bdcbb4a7092529c6
---
# ADR-0118 확장과 퇴역 후보의 검토 뒤 고침

## 맥락

ADR-0114(#107)의 독립 검토가 여덟 가지(F-1부터 F-8)를 찾았다. 퇴역 후보가 옛 흐름과 다르게 정해지는 경우 셋(F-1, F-2, F-3), 전체 계산이 식별이 읽지 않는 레코드를 읽는 것(F-4), 읽는 행이 레코드 수에 비례한다는 약속이 단위가 작을 때 깨지는 것(F-5), 테스트가 지키지 못한 규칙들(F-6), 문서의 거짓 문장(F-7, F-8)이다. 이 결정은 그 고침이다. ADR-0114는 받아들인 결정이라 고치지 않는다. 바뀌는 문장은 아래 표에 적는다.

## 결정

1. **지지는 객체를 만드는 피드의 참조만 센다(F-1).** 옛 흐름은 객체를 만드는 피드의 행이 살아 있을 때만 객체를 붙잡는 것으로 본다. 값만 더하는 피드(`creates_objects`가 거짓)의 레코드는 객체를 붙잡지 않는다. 그래서 퇴역 후보의 조건 2("서 있는 해결된 참조가 없다")의 참조는 객체를 만드는 피드의 것으로 좁힌다. `ExpandStart.Feeds`(소스, 피드 번호, 만듦, 퇴역)가 피드 표를 준다. 표는 `IdentifyFeedsOf`에서 `ExpandFeedsOf`가 만들고, 같은 (소스, 피드)가 둘이거나, 전체 계산인데 표가 없으면 `CodeInvalidValue`다. **표에 없는 피드의 참조는 지지로 센다.** 모르는 피드의 참조를 지지가 아니라고 보면 객체를 잘못 퇴역시키고, 지지라고 보면 퇴역이 늦을 뿐이다. 이 쪽(늦게 퇴역)이 안전하다. 이 보수적인 처리는 옛 흐름에 없는 우리의 결정이다.
2. **식별이 짝을 남기면 후보를 쓰지 않는다(F-2).** 식별의 센 수에 `Unsettled`(아직 풀리지 않은 짝)가 있으면 어떤 객체도 퇴역 후보로 쓰지 않는다. 풀리지 않은 짝은 레코드가 어느 객체에 묶일지 모르는 상태이고, 그때 "아무 레코드도 서 있지 않다"는 판단은 틀릴 수 있다. 값은 실행의 식별 결과에서 DB로 읽는다. 확장을 이어받은 서버도 같게 판단하려는 것이다. `ExpandCounts.Unsettled`가 그 수를 싣는다. 이 때문에 짝이 남은 실행은 퇴역이 한 실행 늦다. 이 사실은 상태 기계(I-7)가 경고로 드러내야 한다.
3. **객체에 영구 표지를 둔다(F-3).** 옛 흐름은 "한 번 퇴역시키는 피드가 본 객체는 영원히 후보"라는 상태를 메모리에 쥐었다. ADR-0114는 이것을 "실행이 시작할 때 있던 참조가 말하는 것"으로 바꿨는데, 객체를 만드는 두 피드 중 하나만 퇴역시킬 때 순서에 따라 옛 흐름과 퇴역 단계가 달라졌다. 그래서 객체 번호만 가진 표 `object_retire`(생성기 8에서 9)를 더한다. 객체 등록부(`object`)에 열을 더하지 않고 옆에 표를 두는 것은, 옛 생성기의 KG를 맞출 때 독자가 열어 둔 표를 건드리지 않는다는 약속(REQ-680, 잠긴 테스트 `TestIngestAKGMadeByTheGeneratorBeforeIsAdjustedByAddingTheTablesWithoutClosingIt`) 때문이다. `object`에 열을 더하는 `ALTER TABLE`은 독자가 있으면 기다리다 시간 제한에 걸리고, 기다리는 동안 다른 독자까지 막는다. 지시서는 `object`의 열(`retirable`)을 말했으나 이 약속과 맞지 않아 표로 바꿨다. 조건 3은 "영구 표지가 있거나, 남은 참조가 `retirable`이다"가 된다. 표지는 적용 단계가 부르는 `MarkRetirableObjects`가 이 실행의 `retirable` 참조가 가리키는 객체에 쓰고, 한 번 쓰면 남는다(두 번째 부름은 0을 센다). 한 쪽에서만 퇴역하는 두 피드가 어느 순서로 놓아도 퇴역은 옛 흐름과 같은 단계에서 일어난다(검토자의 두 순서 테스트를 옮겼다).
4. **전체 계산은 식별이 읽는 레코드만 취한다(F-4).** 규칙 `all`은 `raw_record`의 모든 열린 레코드가 아니라, 계획을 받은 소스의 것으로 데이터셋의 지금 선언 개정(`decl_rev`)과 같은 것만 실행의 레코드로 만든다. 식별이 읽지 않는 레코드를 읽으면 4단계에 매핑이 없는 레코드가 간다. 점진 확장의 집합은 전체 계산의 집합에 들고, 퇴역 후보는 같다.
5. **읽은 키는 실행이 쥔다(F-5).** ADR-0114는 단위 안에서만 같은 키를 한 번 읽었다. 단위가 m보다 작으면 단위마다 한 번씩 읽어서 합이 m에 비례하지 않았다. 그래서 `calc_xkey`(종류, 번호, 해시. 생성기 9)를 두고, 키를 읽는 문장이 `INSERT ... ON CONFLICT DO NOTHING RETURNING`으로 키를 적으면서 **적은 키만** 읽는다. 앞 단위가 적은 키는 읽지 않는다. 쓰기와 읽기가 한 문장이고 단위가 한 트랜잭션이라 키를 읽은 단위가 없던 일이 되면 키도 없던 일이 된다. 규칙마다 키의 종류가 따로라서 건너뛴 규칙은 자기 키만 못 읽은 채 남긴다. 단위는 레코드 번호의 범위(`OFFSET`으로 자른 행의 수)이고 `old` 규칙은 `calc_object`가 표지다. 레코드를 더하는 `INSERT`는 이미 있는 레코드를 먼저 `NOT EXISTS`로 거른다. 그냥 `ON CONFLICT`로 거르면 거른 줄도 번호를 쓰기 때문이다. 그래서 레코드 번호의 가장 큰 수는 레코드 수와 같다.
6. **테스트(F-6).** 검토자의 테스트를 옮기고(아래 대응), 스케일 테스트는 단위가 m보다 작은 실행과 한 단위 실행을 모두 하고, 모든 단위가 읽은 행을 합해서, 읽는 여덟 문장 모두의 m과 3m을 잰다(3.4배 이하 단언은 그대로다).
7. **문구(F-7, F-8).** ADR-0114의 "진 링크" 문구는 "(나) 시드와 격리의 계약"으로 바로잡고, ADR-0114 표의 "다른 소스 — 같은 결과다"는 사실에 맞게 아래 표로 고친다. `RunExpand`의 죽은 검사(`Stage` 확인)는 잠긴 테스트(`exp_run_test`)이 가짜 구현으로 그 검사를 지키므로 지우지 않았다. 지우려면 그 테스트의 단언을 지워야 한다.

## 고친 것 (ADR-0114의 바뀌는 문장)

| ADR-0114 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| 결정 3 조건 2 | 이 실행의 밖에 있거나 다시 읽기만 하는 열린 레코드가 가진 해결된 참조가 없다 | 객체를 만드는 피드의 참조만 센다. 표에 없는 피드의 참조는 센다 (결정 1) |
| 결정 3 조건 3 | 남은 참조가 `retirable`이라고 말한다 | 영구 표지가 있거나 남은 참조가 `retirable`이다 (결정 3) |
| 결정 3 표 | 다른 소스의 레코드가 서 있는 객체: 그 소스의 행이 살아 있으면 지지가 있다(옛), 같다(새), 같은 결과다 | 같은 결과가 아니다. 옛 흐름에는 소스 사이의 지지가 없다. 새 흐름은 다른 소스의 객체를 만드는 피드의 참조도 지지로 세므로 더 보수적이다. 값만 더하는 피드의 참조는 새 흐름에서도 지지가 아니다 (결정 1) |
| 결정 3 표 | `retirable`이 서는 때: 실행이 시작할 때 있던 참조가 말하는 것 | 영구 표지로 옛 흐름의 "한 번 서면 계속"과 같다 (결정 3) |
| 결정 5 | 스키마를 바꾸지 않는다(생성기 7) | 생성기 9: 표 `object_retire`, `calc_xkey` (결정 3, 5). 둘 다 새 표이고 기존 표를 바꾸지 않는다. 세대 8은 ADR-0115의 색인이다 |
| 결정 6 | 단위 안에서 공유하는 키, 객체, 주장을 한 번만 읽는다 | 실행 전체에서 한 번 읽는다. 단위가 m보다 작아도 합이 m에 비례한다 (결정 5) |
| 결정 6 | 같은 키, 객체, 주장은 DISTINCT로 줄인 CTE로 한 번만 읽는다 | DISTINCT가 읽는 양을 지킨다는 말은 거짓이었다. 네 문장(`old`, `key_old`, 한도의 객체, `claim`)에서 DISTINCT를 빼 보아도 읽는 행이 같고 어느 테스트도 달라지지 않았다. 계획이 중복을 반결합으로 한 번 읽고 읽은 키 표의 `ON CONFLICT`가 중복을 거른다. 읽는 양을 지키는 것은 읽은 키 표와 `NOT EXISTS`이고, DISTINCT는 중복을 일찍 줄일 뿐이라 테스트로 지키는 약속에서 뺀다 (결정 5) |
| 알려진 한계 | 단위를 넘어 같은 키를 다시 읽는다 | 고쳤다 (결정 5) |
| 알려진 한계 | 한도에서 진 링크(`link` 테이블에 없는 것)의 부활은 4단계 이후의 몫이다 | 부정확했다. 진 링크를 다시 읽는 일은 설계 3절의 (나) 시드와 4절의 격리가 맡는 계약이다. 4단계와 5단계가 진 링크의 시작 참조를 **미해결**로 남기면 다음 실행의 (나)가 그 레코드를 읽는다. 해결로 남기면 읽지 않는다. 이 단계는 이긴 쪽을 읽어 오기만 한다. 그 계약의 테스트는 4단계의 변경이 둔다 (결정 7) |
| REQ-899 | 읽는 행이 m에 비례한다(단위 안) | 모든 단위의 합이 비례한다 |
| REQ-891 | 전체 계산과 점진 확장의 결과는 같다 | 점진 확장의 집합이 전체 계산의 집합에 들고 후보는 같다 (결정 4) |

## 검토자의 테스트와 옮긴 테스트

| 검토자의 테스트 | 이 저장소의 테스트 | 지키는 것 |
|---|---|---|
| 값만 더하는 피드의 견줌(짧은 것과 긴 것) | `TestExpAnObjectThatOnlyARecordOfAFeedThatAddsValuesStandsUnderIsRetiredAsTheOldPipelineRetiresIt` | 결정 1. 긴 쪽을 옮겼다 |
| 한 피드만 퇴역시키는 두 피드의 견줌(짧은 것, 긴 것, 퇴역 피드가 마지막에 놓는 것) | `TestExpAnObjectTwoFeedsMakeOfWhichOneRetiresIsRetiredAsTheOldPipelineRetiresItWhicheverFeedLetsGoOfItFirst` | 결정 3. 두 순서를 하위 테스트로 둔다 |
| 식별이 남긴 짝의 후보 | `TestExpWhileTheIdentificationLeavesPairsNoObjectIsACandidateForRetirementWhoeverGoesOnWithTheExpansion` | 결정 2 |
| 같은 번호의 피드를 가진 두 소스 | `TestExpTheReferencesOfRecordsOfTwoSourcesWhoseFeedsHaveTheSameNumberAreRetirableByTheFeedOfTheirOwnSource` | 소스마다 자기 피드 |
| 다른 선언의 레코드와 계획 없는 소스의 레코드 | `TestExpTheExpansionOfEverythingTakesTheRecordsTheIdentificationReadsAndNoOther` | 결정 4 |
| 끝을 다투는 링크(지금 찾는 끝, 옛 링크의 끝)와 시작을 다투는 링크(지금 묶인 객체) | `TestExpTheRecordOfALinkThatEndsAtTheObjectAnEndKeyOfARecordFindsIsReadAgain`, `TestExpTheRecordOfASecondLinkThatEndsWhereTheLinkOfAChangedRecordEndedIsReadAgain`, `TestExpTheRecordOfALinkThatStartsAtTheObjectAChangedRecordIsNowBoundToIsReadAgain` | 규칙 (마)의 세 갈래 |
| 단위를 넘는 공유, 레코드 번호의 틈, 한 단위의 규모 | `TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords`, `TestExpTheNumbersOfTheRecordsOfARunAreAsManyAsTheRecordsWhateverTheRulesLookedAt`, `TestExpAKeyThatTwoUnitsHaveIsReadByTheFirstOfThemAndByNoOtherAndTheKeysAreOfTheRun` | 결정 5 |
| `ExpandCards`의 1:1, 1:N, N:1 | `TestExpTheEndsThatALinkTypeHoldsToOneAreTheOnesItsOneToOneOneToManyAndManyToOneBindingsSay` | 한도의 해석 |
| `ExpandPlace.Unit` | `TestExpTheUnitsDoneAreTheUnitsTheExpansionWentByAndTheRunBeginsAtTheFirstRule` | 단위 수 |

검토자의 테스트에 없던 것으로 이 결정이 더한 테스트: `TestExpTheMarkOfTheRegistryIsOnTheObjectsThatARetirableReferenceOfTheRunPointsAtAndStays`(결정 3의 표지), `TestExpAReferenceOfAFeedThatOnlyAddsValuesIsNoSupportAndOneOfAFeedThatIsNotKnownIs`(결정 1의 표에 없는 피드), `TestExpTheFeedsOfTheExpansionAreTheOnesTheIdentificationIsGivenAndAFeedGivenTwiceOrNoFeedForTheExpansionOfEverythingIsRefused`(피드 표의 검증), 세대 테스트 둘(`exp_generation_test.go`).

옮기지 않은 것: 비운 식별의 견줌(통과했던 것), 시드 계약의 두 테스트(진 링크의 참조 상태는 4단계의 계약이고 그 테스트는 4단계의 변경이 둔다), 무관한 10만 줄을 읽지 않는 테스트(통과했고 시간이 크다).

## 옛 흐름과 다른 점

- 옛 흐름에는 소스 사이의 지지가 없다. 새 흐름은 다른 소스의 객체를 만드는 피드의 참조도 지지로 세서 더 보수적이다(결정 1).
- 표에 없는 피드의 참조를 지지로 세는 것은 옛 흐름에 없는 우리의 결정이다(결정 1).
- 식별이 짝을 남긴 실행은 퇴역이 한 실행 늦다(결정 2). 옛 흐름은 짝이 있어도 퇴역한다.

## 잠긴 테스트의 변경

프런트매터 `amends_tests`에 적은 시험들이다. 건별 이유는 `test_change_reason`에 있다. 단언을 지운 것, 문턱을 넓힌 것은 없다.

## 이유

- 퇴역은 되돌릴 수 없는 쪽의 일이라 모를 때는 퇴역하지 않는 쪽으로 기운다(결정 1, 2).
- 영구 표지는 옛 흐름의 메모리 상태를 DB에 옮긴 것이라 이어받은 서버가 같은 판단을 한다(결정 3).
- 읽은 키를 실행이 쥐면 단위의 크기와 상관없이 같은 키를 한 번만 읽는다. 한 문장으로 적고 읽으므로 두 서버가 같은 키를 읽는 경쟁도 한쪽만 이긴다(결정 5).

## 대안과 기각 사유

- **단위를 키의 범위로 자른다.** 레코드 번호의 범위가 `calc_record`의 기본키로 읽혀 가장 싸고, 키를 적는 문장으로 같은 효과를 얻는다.
- **`calc_end`의 인덱스를 더한다.** 단위를 넘어 읽는 것은 읽은 키 표로 없앴으므로 필요 없다.

## 알려진 한계

- 단위 크기보다 키가 더 많으면 `calc_xkey`가 커진다. 실행이 끝나면 계산 테이블과 함께 비운다.
- 식별이 짝을 남긴 실행의 퇴역 지연은 경고로 드러내야 한다(I-7의 몫).

## 결과

- 생성기 버전이 9다. 8에서 올리는 단계는 두 개(표 `object_retire`, 표 `calc_xkey`)이고 모두 새 표를 `IF NOT EXISTS`로 만들어서 두 번 해도 같고 기존 표를 잠그지 않는다.
- 적용 단계(7단계)가 `MarkRetirableObjects`를 부르게 이어 두는 일은 이 결정의 범위 밖이다. 적용과 상태 기계를 잇는 변경(I-7)이 한다. 그 전에는 표지가 쓰이지 않아 조건 3은 남은 참조의 `retirable`만으로 판단한다.
