---
title: ADR-0119 객체 다시 계산에서 건너뛴 객체의 처리 — 만들 수 없는 새 객체는 되돌리고, 있는 객체는 그대로 두며 알린다, 한도와 이어 가기 보완
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/rcp_left_make_test.go
  - backend/internal/pgingest/rcp_left_test.go
  - backend/internal/pgingest/rcp_kill_test.go
  - backend/internal/pgingest/rcp_conflict_parity_test.go
  - backend/internal/pgschema/rcp_left_generation_test.go
  - backend/internal/graph/rcp_left_stagedrows_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0115-recompute-objects-step-of-the-new-ingestion-flow.md
updates_docs:
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0115"
    what: 건너뛴 객체의 규칙(4항, 8항), 한도의 값과 메모리 근거(8항), 이어받은 서버가 쓰는 시각과 체계(3항), 4단계가 가진 문제 종류(9항), 생성기 버전(10항)
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgschema::TestRcpDiffFromGeneratorSevenToEightAddsTheTwoIndexesOfTheComputationOfTheObjectsAndNothingElse
  - go:backend/internal/ingest::TestRcpTheRunOfTheStepBeginsOnceDoesThePagesAndStopsAtTheFirstThingThatFails
  - go:backend/internal/pgingest::TestRcpARunStoppedBetweenPagesGoesOnFromTheNextPageAndStagesWhatOneThatDidNotStopStages
  - go:backend/internal/pgingest::TestRcpTheClaimRuleOfTheExpansionIsNeededForTheObjectsThatHoldAClaimToBeComputed
  - go:backend/internal/pgingest::TestRcpTheComputationIsRefusedBeforeTheExpansionIsDoneAndAPageThatIsNotTheNextOneIsRefused
  - go:backend/internal/pgingest::TestRcpTheObjectsTheRunTouchedAreTheSameWhenEveryRecordIsComputedAndTheOthersAreWhatTheRunBeforeMade
  - go:backend/internal/pgingest::TestRcpThePageSizeDoesNotChangeWhatIsStaged
  - go:backend/internal/pgingest::TestRcpTheReportIsWhatTheStatementsWroteAndAnObjectWithoutRecordsOrWithOnesThatCannotBeReadIsLeft
test_change_reason: 단언을 줄이거나 임계값을 넓힌 것은 없다. (1) TestControlReservesThePartitionPrefix는 생성기 버전의 숫자를 단언하므로 9에서 10으로 바꾼다(이 결정 7 — 세대 9는 먼저 병합된 ADR-0118의 확장 표다). 파티션 접두어를 예약한다는 단언은 그대로다. (2) TestRcpDiffFromGeneratorSevenToEight…는 이제 빌드의 생성기가 8보다 커서 "빌드의 매니페스트를 7과 견준다"가 단계 두 개만 나오지 않는다. 그래서 견주기 전에 다음 쪽 버전을 8로 두는 줄을 더하고, 빌드의 버전이 8 이상이라는 조건을 단언으로 더했다. 단계가 인덱스 둘이고 한 번 더 돌려도 같으며 표가 없는 스키마를 그대로 둔다는 단언은 그대로다. (3) TestRcpTheRunOfTheStepBeginsOnce…는 테스트용 가짜 저장소가 자리(RecomputePlace)에 시작 때의 시각을 돌려주게 했다. 이어받는 서버는 시작한 시각과 같은 것을 주어야 한다는 새 규칙(결정 4) 아래에서 가짜가 이전 시각 없이 자리를 돌려주면 이 테스트가 시작하지 않은 경우를 벗어난다. 단언은 그대로다. (4) pgingest의 rcpReset(도우미)은 이 단계가 새로 쓰는 표(calc_left)도 비운다. 이 도우미를 쓰는 여섯 테스트(TestRcpARunStopped…, TestRcpTheClaimRuleOfTheExpansion…, TestRcpTheComputationIsRefusedBefore…, TestRcpTheObjectsTheRunTouched…, TestRcpThePageSizeDoesNotChange…, TestRcpTheReportIsWhatTheStatementsWrote…)는 지문만 바뀌고 단언은 하나도 바뀌지 않았다. 도우미가 비우지 않으면 다시 도는 세계에서 calc_left의 기본키가 겹쳐 테스트가 실패한다.
decision_hash: 928419f282d1e307
---
# ADR-0119 객체 다시 계산에서 건너뛴 객체의 처리

## 맥락

ADR-0115(#110)가 4단계의 객체 쪽을 정했다. 독립 검토(F-1부터 F-8)가 이 단계의 건너뜀 규칙에서 구멍을 찾았다. 한 객체의 줄이 한도를 넘거나 읽을 수 없으면 "이번 실행에서 그대로 둔다"고 했는데, 그 객체가 이번 실행이 새로 만들 객체이면 그대로 둘 것이 없다. 등록부에 없는 객체를 가리키는 참조와 `calc_newobj`, `calc_newkey`, `calc_bind`, `calc_ask`의 행이 남아 뒤 단계가 없는 객체를 읽다가 멈추거나, 그 객체의 레코드가 문제 없이 풀리지 않은 채 조용히 사라진다. 있는 객체를 건너뛰어도 어느 객체를 왜 건너뛰었는지 남지 않는다. 한도는 페이지의 줄 수보다 컸고, 이어받은 서버는 시작 때의 시각과 체계를 다시 받는 것에 기대어 다른 값으로 이어 갈 수 있었다. 검토는 또 살아남은 변이 스무여 개로 테스트가 어디를 지키지 않는지 보였다.

## 결정

1. **만들 수 없는 새 객체는 되돌린다.** 이번 실행이 만들 객체(`calc_newobj`에 있고 등록부에 없는 것)가 건너뛴 객체가 되면 같은 트랜잭션에서 되돌린다. 그 객체를 가리키던 식별 참조와 주장 참조는 대상 없이 `unresolved`가 되고(퇴역 후보 아님), 그 참조의 레코드마다 문제 하나가 남는다. `calc_newobj`, `calc_newkey`(객체 번호와 문맥 번호 모두), `calc_bind`, `calc_ask`(부모 번호나 답한 참조), `calc_object`에서 그 객체를 이름 대는 행을 지운다. 문맥 부모가 되돌려지면 그 부모를 문맥으로 가진 키의 객체도 되돌린다(재귀, 깊이 제한 없음). SQL 불변식 테스트는 "등록부에도 이번 실행에도 없는 객체를 이름 대는 행이 없다"를 모든 단계의 표에서 본다.
2. **문제의 종류는 새로 만들지 않는다.** 고정 목록(CHECK와 `core.ingest`의 거울 목록)을 넓히지 않는다(REQ-680의 고정 약속과 부딪히지 않는다). 읽을 수 없는 객체와 빈 객체의 레코드는 `shape_changed`, 이름이 없는 객체는 `missing_name`, 한도를 넘은 객체는 `identity_conflict`(매핑의 식별 키가 한 객체 밑에 너무 많은 레코드를 둔다는 뜻), 부모가 되돌려진 객체는 `context_parent_missing`이다. 문장은 레코드나 셀의 내용을 담지 않고 레코드의 데이터셋과 키와 무엇이 잘못인지만 말한다.
3. **있는 객체는 그대로 두고 알린다.** 등록부에 있는 객체를 건너뛰면(한도, 읽을 수 없음, 빈 객체) 객체는 그대로이고, `calc_object`의 행은 퇴역 후보(`why = 4`)가 되지 않으며 `stage_object`에 행이 없다. 뒤 단계는 `stage_object`에 행이 없고 후보가 아닌 객체를 그대로 둔다. 건너뛴 객체마다 `calc_left`(생성기 버전 9의 새 표)에 한 행(이유 1 빈 객체, 2 읽을 수 없음, 3 이름 없음, 4 줄이 너무 많음, 5 부모가 되돌려짐, 줄 수, 새 객체였는지, 이유 글)을 쓴다. 한도를 올리고 모든 객체를 다시 계산하면 이 객체들은 계산된다. 계약은 "`calc_left`에 있는 기존 객체는 이번 실행의 `stage_object`에도 퇴역에도 없다"이고, 세 종류(`TooMany`, `Unreadable`, `Empty`) 모두 `why = 4`가 아님을 테스트가 단언한다. 보고의 `Left`는 `TooMany`와 `Unreadable`의 합이고 `Isolated`는 되돌린 객체의 수(등록부에 없어 읽을 수 없는 객체와 부모 때문에 되돌린 것을 포함)다.
4. **이어받은 서버는 시작한 값을 쓴다.** 자리(`RecomputePlace`)가 시작 때의 시각(`Seen`)과 체계 번호(`Schemes`, 이름순의 `이름=번호;` 글)를 돌려준다(비교 가능한 값이다). `RunRecompute`가 이어받을 때 시작 값이 자리와 다르면 페이지를 읽기 전에 `kernel.CodeConflict`로 거절한다. 문서가 두 시각을 말하거나 모르는 체계의 식별자가 빠지는 일이 없다.
5. **한도는 페이지다.** 한 객체의 줄 수 한도 기본값은 한 페이지의 줄 수(`DefaultRecomputeLines`)와 같고, 명시한 `ObjectMax`가 페이지의 줄 수보다 크면 `CodeInvalidValue`로 거절한다(`Lines`가 기본이면 기본 페이지와 견준다). 한 페이지는 한 객체의 줄을 모두 담으므로 페이지의 메모리가 한도로 묶인다. 측정(`TestRcpTheMemoryOfTheBiggestObjectAPageHoldsIsBoundedByTheNumberOfLinesOfAPage`, 줄 10,000개)은 가장 큰 객체를 만들 때 줄들이 차지하는 것 위로 23.5 MB(값이 모두 같을 때), 27.3 MB(모두 다를 때), 37.2 MB(셀이 200바이트일 때)가 더 들고 최대치는 30.8, 34.8, 46.4 MB다. ADR-0115의 "수십 MB"는 이 값으로 바꾸고, 테스트의 임계값은 줄 위로 100 MB다. 기계에 따라 다르므로 임계값에는 여유가 있다.
6. **살아남은 변이를 죽이는 테스트를 더한다.** 변이 M04, M08, M09, M11, M16, M17(읽을 수 없음 사유 글로), M18, M19, M20, P08, P11, P13, P14, P16, P19, P20, P22b, P24, P25, G1b, G5를 죽이는 테스트이다(보고에 변이마다 테스트의 이름을 적는다). `source_conflict`는 옛 흐름이 같은 수집 사례에서 내는 문제와 같은 (데이터셋, 키) 목록을 낸다는 테스트(`TestRcpTheSourceConflictsAreTheOnesTheOldPipelineReportsFromTheSameCaptures`)을 더해 ADR-0115의 옛 흐름과 다른 점 5의 "옛 흐름과 견주지 않고"를 닫는다.
7. **생성기를 10으로 올린다**(세대 9는 먼저 병합된 ADR-0118의 확장 표). `calc_left`를 만드는 단계(`leftSteps`, 두 번 해도 같고 `calc_record`가 없는 스키마는 그대로 둔다)를 더한다. `calc_left`는 계산 표라서 `ComputeTables()`에 들어 한 번의 실행이 끝나면 다른 계산 표와 함께 비워진다.
8. **`StepFourProblemKinds()`는 단계가 실제로 내는 종류만 돌려준다.** 객체 쪽의 `source_conflict` 하나다. 링크의 `link_target_missing`, `link_target_ambiguous`, `cardinality`는 링크를 만드는 단계(I-5b2)가 더한다. 같은 상수에서 가져와 이름이 어긋날 수 없다. 건너뛴 객체의 레코드에 남는 `shape_changed` 등은 그 레코드가 속한 단계의 종류이고 여기서 소유하지 않는다.

## 고친 것 (ADR-0115의 바뀌는 문장)

| ADR-0115 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| 3항 (시작) | 시각을 실행이 정해 주므로 이어받은 서버가 같은 문서를 쓴다 | 이어받은 서버가 다른 시각이나 체계를 주면 거절하고, 자리가 시작 값을 돌려준다 (결정 4) |
| 4항 (레코드를 읽는 계획) | 읽을 수 없는 객체는 이번 실행에서 그대로 둔다 | 새 객체는 되돌리고 기존 객체는 그대로 두며 `calc_left`에 적는다 (결정 1, 3) |
| 8항 (한도) | 건너뛰고 폐기하거나 바꾸지 않는다 | 기존 객체만 그대로이고, 새 객체는 만들어지지 않으며 되돌린다. 퇴역 후보가 아님을 테스트가 단언한다 (결정 1, 3) |
| 8항 (한도) | 한도 100,000, 레코드 10만 개의 셀은 수십 MB | 한도는 페이지의 줄 수(기본 10,000)이고 측정은 줄 위로 23.5에서 37.2 MB다 (결정 5) |
| 8항 (한도) | 한도는 페이지보다 크다 | 한도는 페이지와 같고 그보다 큰 명시값은 거절한다 (결정 5) |
| 9항 (문제) | `StepFourProblemKinds`가 링크의 종류의 이름을 말한다 | 단계가 내는 종류만 돌려준다 (결정 8) |
| 10항 (생성기) | 8로 올린다 | 10으로 올린다. `calc_left`를 더한다 (결정 7). 세대 9는 ADR-0118의 확장 표다 |
| 알려진 한계 | 건너뛴 객체는 `Left`로 센다 | `Left`는 그대로이고 `calc_left`와 `Isolated`가 더해진다. 드러내는 일은 I-7이다 |

## 잠긴 테스트의 변경

단언을 줄이거나 임계값을 넓힌 것은 없다. 변경한 잠긴 테스트는 `amends_tests`에 있다. 하나는 숫자(생성기 버전)이고, 하나는 견주는 쪽 버전을 8로 고정하는 줄과 하한 단언을 더한 것, 하나는 가짜 저장소의 시각이다. `rcpReset`은 새 표를 비우는 도우미라서 테스트의 단언은 그대로다.

## 이유

- 새 객체를 건너뛰는 것은 "그대로 둔다"가 아니라 "만들지 않는다"이고, 이미 가리키는 곳을 같은 트랜잭션에서 거두지 않으면 뒤 단계가 없는 객체를 읽는다. 고아 참조를 없애는 일은 한 곳(`takeBack`)에서 하고 SQL 불변식으로 지킨다.
- 있는 객체를 건너뛸 때 퇴역으로 보내지 않는 것은 "읽지 못함"이 "사라짐"과 같지 않아서다. 한도를 올리고 다시 계산하면 풀리는 상태다.
- 문제의 종류를 늘리면 CHECK, 거울 목록, 그 목록의 개수 테스트를 모두 고쳐야 하고 REQ-680과 부딪히므로 기존 종류의 뜻에 맞춰 쓴다.
- 한도를 페이지에 묶으면 메모리의 상한이 한 값으로 정해져 따로 따질 값이 없다.

## 대안과 기각 사유

- **새 문제 종류(`object_left` 등).** CHECK와 거울 목록을 넓히고 REQ-680의 약속을 건드린다. 기각.
- **건너뛴 새 객체의 참조를 풀리지 않은 채 두지 않고 그냥 지운다.** 레코드가 이유 없이 사라진다. 기각.
- **`calc_object`에 이유 열을 더한다.** 이 표는 모든 단계가 읽어서 넓히면 생성기 단계가 크다. 옆의 표가 작다.

## 알려진 한계와 넘김

- 기존 객체가 한도를 넘어 건너뛰어지면 한도를 올려 전부 다시 계산할 때까지 낡은 채로 있다. 드러내는 일(경고, 화면)은 I-7이다.
- **I-7로 넘긴다(F-6).** 한 레코드가 두 객체 밑에 서고 한 객체만 이번에 계산될 때, 계산되지 않은 다른 객체의 `source_conflict`가 정리에서 사라지는지는 이 PR이 문제를 쓰기만 하고 지우지 않아 확인하지 못했다. I-7이 정리를 만들 때 이 경우를 테스트로 정한다.
- 한 객체가 한도를 넘는 것과 부모가 되돌려진 것이 겹칠 때 `calc_left`의 이유는 단계가 먼저 만난 쪽이다(페이지의 크기에 따라 다를 수 있다). 되돌려지는 객체의 집합은 같다. 테스트는 집합과 불변식을 단언하고 이유를 견주지 않는다.
- 한도의 기본값은 `Lines`가 기본일 때 정해지고, 기본 `ObjectMax`를 쓰면서 `Lines`만 줄이면 한도가 페이지보다 클 수 있다(명시값만 거절한다). 이때 메모리의 상한은 `Lines`가 아니라 기본 한도다.
- 링크의 문제 종류와 링크의 계산은 I-5b2다.
- 변이 P25(`record_ref`에서 읽는 주장 가지가 `state = found`만 읽는 조건을 푼 것)는 동등 변이다. 잃은 주장을 가진 레코드는 `CalcWhyUnresolved`로 실행마다 다시 읽혀 `calc_ref`로 옮겨 가므로 그 가지에 닿을 수 없다. `calc_ref` 가지의 같은 변이는 테스트가 잡는다.
- `Isolated`는 읽을 수 없는 객체(등록부에도 새 객체에도 없는 번호)와 연쇄로 되돌린 객체를 모두 센다.

## 결과

- 만들 수 없는 새 객체는 흔적 없이 되돌려지고 레코드는 문제와 함께 남는다.
- 기존 객체의 건너뜀은 `calc_left`로 남고 퇴역이 되지 않는다.
- 이어 가기와 한도의 모호함이 닫혔고, 살아남은 변이가 테스트에 걸린다.
