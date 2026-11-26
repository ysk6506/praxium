---
title: ADR-0116 식별의 두 번째 검토 뒤 고침 — 남기는 단위는 묶음, Go 정착의 읽기와 임대, 무작위 사슬과 끊기 시험
kind: decision
status: active
enforced_by:
  - TLOCK001
  - backend/internal/pgingest/idn_follow_test.go
  - backend/internal/pgingest/idn_endsearch_test.go
  - backend/internal/pgingest/idn_second_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0108-identify-step-slots-rounds-and-groups-in-sql.md
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0108"
    what: 결정 4의 뒤 라운드에서 이웃을 남기는 방법(쌍에서 묶음으로)과 Go 정착의 읽기·임대·끊기 시험, 결정 7의 분석하는 때, 알려진 한계의 갈라짐과 임대 문장
amends_tests:
  - go:backend/internal/pgingest::TestIdnAChainOfRecordsInAnyOrderIsOneObjectWhenTheTurnsEndBeforeItsEndAndGoEndsTheSearchAndNothingIsLeftWhenItCannotHoldIt
  - go:backend/internal/pgingest::TestIdnARunThatStopsWhileGoEndsTheSearchForTheGroupsGoesOnAndMakesWhatARunThatDoesNotStopMakes
  - go:backend/internal/pgingest::TestIdnTheLabelsOfTheTurnsBeforeTheLastTwoAreTakenAwayAndTheRowsOfThemAreAtMostTwoForEachPair
  - go:backend/internal/pgingest::TestIdnTheIdentificationOfARunThatHasBegunOrWhoseComparisonIsNotDoneIsNotBegunAndWhatItMadeIsNotChanged
  - go:backend/internal/pgingest::TestIdnTheCallsOfTheIdentificationThatAreMadeOutOfOrderOrOnARunThereIsNotAreRefused
  - go:backend/internal/pgingest::TestIdnARunThatDoesNotOwnTheTablesOfTheComputationNeitherBeginsNorGoesOnWithTheIdentification
  - go:backend/internal/pgingest::TestIdnARunThatStopsGoesOnFromTheNextUnitAndMakesWhatARunThatDoesNotStopMakes
test_change_reason: 단언을 지우거나 약하게 하거나 문턱을 넓힌 것은 없다. 세 갈래다. 첫째(결정 4) — 도우미 `idnShuffled`가 행 순서만이 아니라 자연 키를 무작위로 순열한다. 쌍 번호는 자연 키를 따르므로 이제 사슬의 순서와 무관하다. 이 도우미를 쓰는 세 테스트(`TestIdnAChainOfRecordsInAnyOrder...`, `TestIdnARunThatStopsWhileGoEnds...`, `TestIdnTheLabelsOfTheTurnsBeforeTheLastTwo...`)의 지문이 바뀌었다. 앞의 것은 사례를 바꿨다. 쌍 번호가 사슬 순서이던 1,600개와 6,000개(기본 회 안에 15회, 31회로 끝나 Go를 한 번도 타지 않던 가장 좋은 경우)를 무작위 400개와 1,600개(기본 회 40에 닿고 Go가 끝냄)로 바꾸고, 모든 사례에서 Go가 읽은 쪽이 하나 이상임을 새로 단언한다. 한 객체, `NewObjects`, `Unsettled`, `Unresolved`의 단언과 회 2·Go 상한 1의 두 사례는 그대로다. 무작위 6,000개는 Go가 열한 쪽을 읽어 약 17초라 테스트 시간 때문에 넣지 않았다(지시서). 둘째(결정 5) — 끊기 하네스 `idnSettleCut`이 이름을 붙이지 않던 호출(키 다시 만들기의 쓰기, 답의 쓰기)도 목록에 적어 목록의 자리가 끊는 번호와 같다. 전에는 번호가 어긋나 Go의 쓰기를 한 번도 끊지 않았다. `TestIdnARunThatStopsWhileGoEnds...`는 Go의 쓰기 쪽 크기를 100으로 낮춰(`IdentifyStart.SettlePut`) 세 쪽을 쓰게 하고, 같은 끊는 자리를 두 번 돌지 않으며, 끊긴 자리에 Go의 읽기와 쓰기가 각각 둘 이상 있음을 새로 단언한다. 끊지 않은 실행과 같다는 단언은 그대로다. 이 하네스의 `fail`은 메서드 `IdentifyStep`에서 불려서, 잠금 도구가 이름으로 `IdentifyStep`을 따라가는 다른 넷(`TestIdnTheIdentificationOfARunThatHasBegun...`, `TestIdnTheCallsOfTheIdentificationThatAreMadeOutOfOrder...`, `TestIdnARunThatDoesNotOwnTheTables...`, `TestIdnARunThatStopsGoesOnFromTheNextUnit...`)도 지문만 바뀌었고 본문과 단언은 한 글자도 바뀌지 않았다. 셋째 — 없음(새 테스트는 `pnpm lock-tests`가 더한다).
---
# ADR-0116 식별의 두 번째 검토 뒤 고침

## 맥락

ADR-0108(#103)의 독립 검토가 병합 전 재확인에서 일곱 가지(G1~G7)를 찾았다. G1(패키지 테스트 시간)은 병합 전에 고쳤다. 이 결정은 G2~G5의 고침과 G6의 정정이다. G7은 하지 않고 넘긴다(아래 "넘기는 것"). ADR-0108은 받아들인 결정이라 고치지 않는다. 바뀌는 문장은 아래 표에 적는다. 스키마(생성기 세대)는 바꾸지 않는다.

## 결정

1. **남기는 단위는 묶음이다(G2).** 전에는 뒤 라운드가 슬롯을 만들 때, 앞 라운드에서 남긴 쌍과 키를 **직접** 나누는 쌍만 남겼다(`leaveNeighbors`). 남긴 쌍은 슬롯이 없어서, 그 쌍과만 키를 나누는 같은 라운드의 쌍은 찾지 못하고 혼자 새 객체를 만들었다. 다음 실행에서 사슬이 두 객체 사이에 끼어 갈라지고 `identity_conflict`가 났다(검토자의 재현: 둘째 실행 뒤 셋째 실행에서 문제 6건). 그래서 슬롯 때 남기지 않고, **결정의 첫 문장이 묶음(라벨)을 통째로 남긴다.** 뒤 라운드의 쌍도 모두 슬롯을 갖고 묶음 찾기에 들어가므로, 같은 라운드에서 키로 이어진 쌍은 이미 한 묶음이다. 그 묶음의 키 가운데 하나가 앞 라운드의 슬롯 중 신원 참조(남김 또는 묶을 수 없음)를 가진 쌍의 키이면 묶음 전체를 남긴다(`neighborsSQL`의 `nb`). 회가 끝나지 않아 온전하지 않은 묶음(`bad`)과 합쳐(`gone`) 한 문장으로 남기고 `Unsettled`와 `Unresolved`로 센다. 남긴 묶음의 쌍은 모두 슬롯이 있으므로 그 뒤의 라운드도 같은 방법으로 찾는다.
   - **고정점과 그 상한.** 같은 라운드 안의 고정점은 묶음 찾기(회와 Go의 정착)가 이미 구한다. 따로 반복하는 고리가 없다. 묶음 찾기가 회 한도와 Go의 상한 안에 끝나지 않으면 그 조각들은 `bad`로 남는다. 곧 키를 나누는 쌍이 끝없이 이어져도 반복의 상한은 회(40)와 Go의 상한(20만 쌍)이고, 그때의 결과는 "그 묶음 전체를 남김, 문제 없음"이다(`TestIdnAGroupOfALaterRound...`가 뒤 라운드의 사슬이 한 회에 끝나지 않고 Go가 한 쌍만 쥐는 경우를 본다). 라운드를 넘는 쪽은 라운드 수(16)가 상한이다.
   - **언제 하는가.** 처음 남긴 라운드를 `LeftRound`에 적고(전의 `LeftKeys` 대신), 그보다 뒤 라운드의 모든 단위가 이웃을 본다. 라운드 도중에 값이 바뀌지 않으므로 단위의 순서가 결과를 바꾸지 않는다. 앞 라운드의 묶을 수 없던 쌍(`identity_conflict`의 참조)도 "신원 참조를 가진 쌍"으로 보는 것은 전과 같다.
   - **읽는 양.** 키마다 앞 라운드마다 한 번 슬롯의 키 인덱스로 본다(`kk`의 키, 라운드는 최대 16). 한 라운드에서 키를 나누는 쌍은 한 묶음이고 묶음은 한 단위라서 키는 라운드마다 한 번 읽힌다. 쌍마다 보면(전의 `leaveNeighbors`가 그랬다) 키를 m개 쌍이 나눌 때 m의 제곱이 된다(`TestIdnTheKeyOfAGroupIsLookedAtOnce...`, m=100과 300에서 3.5배 + 50 안, 측정 2.9배). 남긴 묶음은 결정의 나머지 문장에서 빠지는데, 문장마다 다시 구하지 않고 묶음의 라벨인 쌍(라벨은 묶음의 가장 작은 쌍 번호다)의 신원 참조를 인덱스로 한 번 본다(`pairsSQL`). 남기는 일이 없는 라운드의 문장은 전과 같다.
   - **이어 가기.** 남기기는 결정 단위의 첫 문장이고 단위와 자리가 한 트랜잭션이다. 끊겨 이어받은 서버는 다음 단위부터 같은 일을 한다(새 테스트가 뒤 라운드의 여러 자리에서 끊고 이어받아 끊지 않은 실행과 같은 상태와 수를 확인).
   - **다음 실행.** 남긴 쌍은 미해결 참조라 다음 실행의 출발 레코드이고, 기본 한도에서 한 객체가 되며 문제가 없다(검토자의 재현을 그대로 단언).
2. **Go의 정착 읽기는 쪽의 범위만 읽는다(G3).** 검토의 측정에서 첫 쪽이 `calc_label` 64만 행(5,000쌍)을 읽었다. 원인은 계획이었다. 회가 끝나 Go에게 넘어갈 때 `calc_label`을 분석하지 않아서, 서버가 마지막 회의 줄이 없다고 보고 쌍 번호로 라벨을 찾는 탐색을 라벨 인덱스(`calc_label_label`)의 범위 읽기와 거르기로 했다(쌍마다 그 회의 라벨을 훑음). 그래서 (1) 회가 끝나 정착으로 넘어갈 때와 (2) Go가 마지막 쪽을 써서 복사로 넘어갈 때 `calc_label`을 분석한다(다른 국면 전환이 이미 하는 일, ADR-0108 결정 7). (2)가 없으면 복사의 단위가 같은 이유로 쌍마다 Go가 쓴 회를 훑었다(복사 문장의 합이 5,000쌍에서 1.0초, 15,000쌍에서 8.7초. 고친 뒤 5,000쌍에서 0.01초). (3) 정착 읽기는 `unsettledSQL`이 이미 읽은 쪽의 쌍과 키(`sk`)에서 바로 고르고, 라벨로 다시 찾지 않는다. 고친 뒤 쪽(500쌍)마다 `calc_label` 1,000행 남짓, `calc_slot` 2,000행 남짓이고 2,000쌍과 6,000쌍에서 같다(`TestIdnGoReadsAPage...`).
3. **정착 읽기가 임대를 늘린다(G3).** Go는 모든 쪽을 읽은 뒤에 쓴다. 쓰기와 다른 단위는 이미 자리의 기록과 함께 임대를 늘리는데(ADR-0108 결정 1, `writeIdent`), 읽기는 늘리지 않아 큰 묶음을 읽는 동안 임대가 끝나면 다른 서버가 가져가 처음부터 다시 읽었다. 그래서 `SettleRead`가 같은 트랜잭션에서 `RenewRun`과 같은 문장(`renewIn`)으로 그 시도의 임대를 늘린다. 포트에 메서드를 더하지 않았다. 3단계 포트의 꼴(ADR-0108 결정 1)은 "모든 쓰기가 시도 번호를 대고, 단위가 자리와 함께 임대를 늘린다"이고, 읽기도 그 단위 가운데 하나가 된 것이다. 시도가 다르면 `CodeRunLeaseLost`로 거절하고 임대를 건드리지 않으며, 국면이 정착이 아니면 `CodeRunStage`로 거절한다.
4. **무작위 사슬(G4).** 테스트 도우미 `idnShuffled`가 자연 키를 무작위로 순열해 쌍 번호가 사슬의 순서와 무관하다. 전에는 행 순서만 섞고 번호가 사슬 순서라서, ADR-0108의 "1,600개 15회, 6,000개 31회"는 가장 좋은 경우였다(ADR-0108이 이미 그렇게 고쳐 적었다). 이제 기본 회로 무작위 400개와 1,600개 사슬을 돌려 한 객체이고 Go가 한 쪽 이상 읽었음을 단언한다(측정: 둘 다 41회, Go 1쪽과 3쪽).
5. **시험의 빈틈(G5).**
   - 끊기 하네스 `idnSettleCut`이 이름을 붙이지 않던 호출도 목록에 적어, 목록의 자리가 끊는 번호다. 시험용 설정 `IdentifyStart.SettlePut`(Go가 한 번에 쓰는 쌍 수, 0이면 제품의 기본값 5,000인 이름 있는 상수 `settlePut`)으로 300개 사슬의 라벨을 세 쪽에 쓰게 하고, Go의 읽기와 쓰기마다 끊어 끊지 않은 실행과 같음을 본다.
   - 여러 객체의 묶음 문장의 두 조건을 각각 지키는 시험을 둔다. 한 키를 두 객체의 쌍이 가지면(`max(kt.c) = 1`) 그 키를 가진 새 레코드는 어느 쪽에도 묶이지 않고 `identity_conflict`다. 쌍의 키가 각각 한 객체이지만 서로 다른 객체이면(`count(DISTINCT kt.obj) = 1`) 묶이지 않는다.
   - 정착의 읽기와 쓰기는 국면이 정착일 때만 받는다(시작 직후, 끝난 뒤 거절).
   - 고친 뒤 변이 M1b, M1c, M2e, M2k와 결정 1~3을 되돌린 변이를 사본에 넣어 새 시험이 잡는지 확인했다(아래 표).
6. **쓰기 양(G6)은 고치지 않고 사실대로 적는다.** 아래 알려진 한계.

## 고친 것 (ADR-0108의 바뀌는 문장)

| 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| ADR-0108 결정 4 "한 라운드에서 남긴 쌍의 이웃도 뒤 라운드에서 남긴다" | 뒤 라운드의 슬롯 때 앞 라운드에서 남긴 쌍과 키를 직접 나누는 쌍을 남긴다(`leaveNeighbors`, `LeftKeys`). 남긴 쌍과만 이어지는 쌍은 못 찾아 갈라지고 `identity_conflict`가 날 수 있다 | 결정의 첫 문장이 앞 라운드에서 남긴 쌍과 키를 나누는 묶음을 통째로 남긴다(`LeftRound`). 갈라짐과 그 `identity_conflict`는 없다 (결정 1) |
| ADR-0108 결정 4 "Go가 끝낸다" | 끊기 시험이 Go의 읽기와 쓰기 앞뒤에서 끊어 확인한다 | 그 시험은 번호가 어긋나 쓰기를 끊지 않았다. 하네스를 고치고 쓰기를 세 쪽으로 나눠 읽기와 쓰기를 각각 둘 이상 끊는다 (결정 5) |
| ADR-0108 결정 7 | 국면이 바뀔 때(복사 뒤, 슬롯 뒤, 라벨 뒤) `ANALYZE` | 회가 끝나 Go에게 넘어갈 때와 Go가 마지막 쪽을 쓸 때도 `calc_label`을 분석한다 (결정 2) |
| ADR-0108 알려진 한계 | "Go의 정착 단계는 쌍 수에 가파르게 늘어나는 읽기를 한 번에 하고, 그동안 임대를 늘리지 않는다" | 쪽마다 쪽의 범위만 읽고(쪽 크기에 비례) 쪽마다 임대를 늘린다 (결정 2, 3) |
| ADR-0108 알려진 한계 | 쓰기 양이 (쌍 수)×(회 수) | 삽입과 같은 수의 삭제가 더해진다 (알려진 한계) |

## 검토자의 재현과 옮긴 테스트

| 검토자의 재현 | 이 저장소의 테스트 | 지키는 것 |
|---|---|---|
| `TestZzr2TheNeighborOfTheNeighborOfALeftChain` | `TestIdnARecordThatSharesAKeyOnlyWithARecordThatIsLeftWithAChainIsLeftWithThemAndTheNextRunMakesThemAllOneObject` | 결정 1. 입력 그대로: 둘째 실행은 아무것도 묶지 않고 문제 없음, 셋째 실행에서 사슬·p0·q0·k0이 한 객체, 문제 없음, `Unsettled 0`, `Unresolved 0` |
| (없음, 예외 상황) | `TestIdnAGroupOfALaterRoundThatSharesAKeyWithAChainThatIsLeftIsLeftWholeEvenWhenItsOwnSearchDoesNotEndAndARunTakenOverLeavesTheSame` | 결정 1의 상한과 이어 가기, 다음 실행에서 한 객체 |
| (없음, 제곱 시험) | `TestIdnTheKeyOfAGroupIsLookedAtOnceInTheEarlierRoundsForAPairTheyLeftAndWhatThatReadsGrowsWithTheGroup` | 결정 1의 읽는 양 |
| `TestZzr2SettleReadStatements`, `TestZzr2SettleReadTime` | `TestIdnGoReadsAPageOfTheGroupsThatAreNotWholeByItsRangeAndWhatAPageReadsDoesNotGrowWithThePairsThatAreLeftToRead` | 결정 2. 지시서의 5,000쌍과 15,000쌍은 이 기계에서 14초라 10초 규칙에 맞춰 2,000쌍과 6,000쌍(약 7초)으로 줄였다. 고치기 전 코드는 이 크기에서도 쪽당 수십만 행을 읽는다 |
| `TestZzr2AStaleAttemptCannotReadOrWriteAtTheEndOfASearch` | `TestIdnGoReadsAndWritesTheEndOfASearchOnlyWhereTheRunIsAtItAndEachReadExtendsTheLeaseOfTheAttemptThatHoldsIt` | 결정 3과 국면 검사(M2k): 쪽마다 임대가 반 기간 넘게 남고 시도 번호가 같음, 잃은 시도는 거절되고 임대를 건드리지 않음 |
| `TestZzr2RandomChainAtTheDefaultTurns` | `TestIdnAChainOfRecordsInAnyOrder...`(사례를 바꿈) | 결정 4 |
| `TestZzr2CutAtEveryCallOfTheSettleOfTwoPages`, `TestZzr2WhereTheCutsOfTheSettleDie` | `TestIdnARunThatStopsWhileGoEnds...`(하네스와 쪽 크기를 바꿈) | 결정 5, M2e |
| (변이 M1b) | `TestIdnARecordOfAGroupOfSeveralObjectsWhoseKeyThePairsOfTwoObjectsHaveIsNotBoundToEitherAndIsAConflict` | 결정 5 |
| `TestZzr2APairWhoseKeysAreOfTwoObjectsIsNotBoundToOne` | `TestIdnARecordOfAGroupOfSeveralObjectsWhoseKeysAreEachOfOneObjectButOfTwoIsNotBoundToEither` | 결정 5 |

## 변이 시험

작업 폴더의 `backend/`를 임시 폴더에 복사해 한 곳만 틀리게 고치고 위 테스트들을 돌렸다(저장소 소스는 그대로). 결과는 "변이 시험 결과" 절에 적는다.

## 이유

- 남기는 목적은 다음 실행이 한 묶음을 한꺼번에 보게 하는 것이다. 쌍 단위로 남기면 남긴 쌍이 슬롯을 갖지 않아 이웃의 이웃이 보이지 않는다. 묶음 단위로 남기면 같은 라운드의 고정점은 이미 있는 묶음 찾기가 구하고, 라운드를 넘는 쪽은 남긴 쌍이 모두 슬롯을 가져 같은 탐색으로 찾는다. 새 표가 필요 없다.
- 남긴 키를 라운드 사이의 표에 이어 가는 방법(검토자의 둘째 방향)은 새 작업 테이블, 곧 스키마 변경(생성기 세대)이 필요하고, 같은 라운드에서 새로 남긴 쌍의 키를 다시 보게 하려면 슬롯 단계에 반복을 더해야 한다. 슬롯 단위는 레코드의 범위로 나뉘어서 그 반복은 단위의 범위를 넘어 읽는다. 묶음 단위로 남기면 읽기가 결정 단위(라벨의 범위) 안에 머문다.
- 통계는 국면이 바뀔 때 주는 것이 이 단계의 규칙이다(ADR-0108 결정 7). 정착과 복사도 국면이다.

## 대안과 기각 사유

- **슬롯 때 남기기를 고정점까지 반복한다.** 위 이유의 둘째.
- **정착 읽기에 계획 힌트를 준다.** 제품 문장에 설정을 넣지 않는다. 통계가 있으면 서버가 쌍 번호 인덱스를 고른다(측정).
- **포트에 `RenewRun`을 더하고 `settleGroups`가 부른다.** 포트를 바꾸면 모든 가짜 구현과 잠긴 테스트가 바뀌고, 단위가 자리와 함께 임대를 늘리는 지금의 꼴과 두 가지가 된다.
- **남긴 묶음을 결정의 문장마다 다시 구한다(전의 `bad`처럼).** 문장마다 키를 앞 라운드마다 다시 본다. 라벨인 쌍의 참조 하나를 보는 것이 싸다.

## 알려진 한계

- 남기기는 앞 라운드의 "신원 참조를 가진 쌍"을 본다. 묶을 수 없어 미해결이 된 쌍(`identity_conflict`)의 이웃도 남긴다(전과 같다). 처음 남긴 라운드(`LeftRound`)가 없으면 보지 않는다.
- 가장 작은 라벨의 전파는 라운드마다 모든 쌍을 회마다 다시 쓰고, 회 T는 T-2회의 줄을 지운다. 그래서 쓰기 양은 (쌍 수)×(회 수)의 삽입과 거의 같은 수의 삭제(죽은 행)다. 1,600쌍 41회에서 삽입 67,200, 삭제 64,000, 남는 줄 3,200(검토의 측정, G6). 바꾸지 않았다.
- 한 묶음은 한 단위라는 한계(ADR-0108)는 그대로다. 남기는 묶음도 한 단위가 통째로 본다.

## 넘기는 것

- **G7 이름 유무의 판단.** Go(`strings.TrimSpace`)와 SQL(`btrim`)이 다르게 판단해 탭과 U+00A0만 있는 이름은 SQL이 "있음"으로 대표를 고르고 Go가 `missing_name`을 낸다. 검토자의 권고(Go가 정해 키에 싣고 SQL이 그것으로 고름)는 받은 키 표와 계산 키 표(`stage_key`, `calc_key`)에 열을 더하는 일이라 생성기 세대를 올려야 한다. 열린 링크 PR도 같은 세대(12)를 쓰고 있어서, 세대 번호와 순서를 설계 책임자가 정한 뒤 따로 한다.
