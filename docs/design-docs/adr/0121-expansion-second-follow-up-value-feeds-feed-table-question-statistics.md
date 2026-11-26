---
title: ADR-0121 확장의 두 번째 검토 뒤 고침 — 값만 더하는 피드의 묶임, 점진 확장의 피드 표, 질문 표의 통계, 읽는 양을 지키는 DISTINCT
kind: decision
status: active
enforced_by:
  - TLOCK001
  - backend/internal/ingest/exp_run_test.go
  - backend/internal/pgingest/exp_valueonly_test.go
  - backend/internal/pgingest/idn_askplan_test.go
  - backend/internal/pgingest/exp_opplan_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0108-identify-step-slots-rounds-and-groups-in-sql.md
  - docs/design-docs/adr/0118-expansion-follow-up-support-mark-and-units-after-review.md
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0118"
    what: 결정 1(지지의 범위를 조건 1의 묶임까지, 점진 확장의 피드 표), 결정 6과 표의 DISTINCT 문장(DISTINCT가 읽는 양을 지키지 않는다는 말), 결과의 세대 표기
  - adr: "0108"
    what: 결정 7의 국면이 바뀔 때 분석하는 작업 테이블(질문 표 `calc_ask`를 더함)과 계획 테스트가 재는 계획
amends_tests:
  - go:backend/internal/ingest::TestExpTheRunOfTheExpansionBeginsOnceDoesTheUnitsInOrderAndReportsAtTheEnd
  - go:backend/internal/ingest::TestExpTheRunOfTheExpansionStopsAtAUnitThatFailsAtARunThatIsNotComputingAndAtACancelledContext
  - go:backend/internal/pgingest::TestExpTheExpansionDoesNotBeginOnARunWhoseIdentificationIsNotDoneAndTheComputationOfEverythingGoesByUnitsOfRecords
  - go:backend/internal/pgingest::TestExpTheRecordOfALinkThatEndsAtTheObjectAnEndKeyOfARecordFindsIsReadAgain
  - go:backend/internal/pgingest::TestExpTheRecordOfASecondLinkThatEndsWhereTheLinkOfAChangedRecordEndedIsReadAgain
  - go:backend/internal/pgingest::TestExpTheRecordOfALinkThatStartsAtTheObjectAChangedRecordIsNowBoundToIsReadAgain
  - go:backend/internal/pgingest::TestExpTheUnitsDoneAreTheUnitsTheExpansionWentByAndTheRunBeginsAtTheFirstRule
  - go:backend/internal/pgingest::TestExpTheCandidatesForRetirementAreTheObjectsTheOldPipelineRetiresInTheSameCaptures
  - go:backend/internal/pgingest::TestExpTheExpansionOfARunOfAWorldThatHasACaseForEveryRule
  - go:backend/internal/pgingest::TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords
  - go:backend/internal/pgingest::TestExpTheNumbersOfTheRecordsOfARunAreAsManyAsTheRecordsWhateverTheRulesLookedAt
  - go:backend/internal/pgingest::TestExpAKeyThatTwoUnitsHaveIsReadByTheFirstOfThemAndByNoOtherAndTheKeysAreOfTheRun
  - go:backend/internal/pgingest::TestExpAnObjectThatARecordOfAnotherSourceStandsUnderIsNotACandidateAndOneThatNoRecordStandsUnderIs
  - go:backend/internal/pgingest::TestIdnEveryStatementOfAUnitReadsTheRegistryByItsIndexesAndWhatItReadsOfItDoesNotGrowWithTheSource
  - go:backend/internal/pgingest::TestIdnWhenEveryRecordIsInTheSamePlaceAUnitReadsOfTheRegistryWhatItDidWhenTheyWereInManyAndNoMore
  - go:backend/internal/pgingest::TestIdnMeasureTheIdentificationOfARunWithAChangeOfOneInTen
  - go:backend/internal/pgingest::TestIdnAGroupOfSeveralObjectsIsReadOnceForEachKeyAndWhatAUnitReadsOfItGrowsWithTheGroupAndNoFaster
  - go:backend/internal/pgingest::TestIdnMeasureTheIdentificationOfAGroupOfManyObjects
  - go:backend/internal/pgingest::TestRcpWhatManyRecordsOfAnObjectShareIsReadOnceSoTheRowsReadGoWithTheNumberOfLines
test_change_reason: 단언을 지우거나 약하게 하거나 임계값을 넓힌 것은 하나도 없다. 두 갈래의 변경이다. 첫째(결정 3, 모든 확장에 피드 표) — ingest의 두 테스트(가짜 구현으로 상태 기계를 보는 것)와 pgingest의 확장 테스트 열하나는 확장을 표 없이 시작하던 입력에 피드 표만 더했다(ingest는 새 변수 `expOneFeed`, pgingest는 이미 있는 `expFeeds`). 단언과 기대값은 그대로다. 음수 크기와 같은 링크 유형 두 번을 거절하는 단언은 표를 함께 주어서, 표가 없어 거절되는 것이 아니라 그 검사로 거절되는지를 그대로 본다. 지문만 바뀐 것은 도우미(`expSteps`, `expCheckResume`, `expCheckCalls`, `expRowsRead`)를 쓰는 테스트다. `TestExpTheCandidatesForRetirementAreTheObjectsTheOldPipelineRetiresInTheSameCaptures`는 표를 받아 값만 더하는 피드의 참조가 지지가 아니게 되었고 옛 `Runner`와 같은 단계를 단언하는 것은 그대로이며 통과한다. 둘째(결정 4, 계획 테스트가 재는 계획) — 측정 도우미 `idnHook`가 측정하는 세이브포인트 안에서 해시 조인, 병합 조인, 통째 읽기를 끈다(`SET LOCAL`, 세이브포인트를 되돌리면 풀려 제품의 문장은 원래 설정으로 돈다). 이 도우미를 쓰는 여섯 테스트(식별의 계획 넷, 그룹 측정 하나, 4단계의 공유 하나)의 지문이 바뀌었고 임계값(1.5배와 40*size, 3.5배와 8*레코드/10, 3.4배)과 통째 읽기 검사는 그대로다. 측정 둘(`TestIdnMeasure...`)은 단언이 없는 측정이고 하나는 -idn.rows가 있을 때만 돈다. 제품이 calc_ask를 분석하자 테스트 크기에서 통계가 해시나 병합 조인으로 calc_record를 통째로 읽는 계획을 골라 `TestIdnEvery...`가 결정적으로 깨졌기 때문이다. 운영 크기에서 서버가 고르는 계획은 새 테스트 `TestIdnTheQuestionsOfARoundAreReadByTheirIndexWithTheStatisticsTheRunGivesThePlanner`가 본다.
decision_hash: 1541c8c0e6451c44
---
# ADR-0121 확장의 두 번째 검토 뒤 고침

## 맥락

ADR-0118(#111)의 독립 검토가 여덟 가지(G-1부터 G-8)를 찾았다. 이 결정은 그중 다섯(G-1, G-3, G-4, G-5, G-8)의 고침이고, 셋(G-2, G-6, G-7)은 뒤의 작업으로 넘긴다. ADR-0118과 ADR-0108은 받아들인 결정이라 고치지 않는다. 바뀌는 문장은 아래 표에 적는다. 스키마(생성기 세대)는 바꾸지 않는다.

## 결정

1. **값만 더하는 피드의 레코드는 바뀌어도 객체를 붙잡지 않는다(G-1).** ADR-0118 결정 1은 퇴역 후보의 조건 2(서 있는 참조)만 객체를 만드는 피드의 것으로 좁혔다. 그런데 값만 더하는 피드의 레코드가 이번 실행에서 바뀌면 식별이 그 레코드를 객체에 다시 묶고(`calc_bind`), 조건 1(이 실행의 레코드가 묶였다)이 그 묶임을 받침으로 셌다. 상태 컬럼이 실행마다 바뀌는 보강 소스가 흔하므로, 만드는 피드가 놓은 객체가 퇴역하지 않고 남았다. 그래서 조건 1도 **객체를 만드는 피드의 묶임만** 센다. 묶임은 식별이 그것으로 쓴 참조(`calc_ref`의 해결된 신원 참조, `calc_ref_target` 인덱스로 객체에서 찾는다)로 읽고, 레코드의 소스와 피드 번호로 피드 표를 본다. 표에 없는 피드의 묶임은 지금처럼 받침이다(ADR-0118 결정 1의 보수적 처리).
2. **식별이 넣은 객체도 판정한다(G-1).** 식별은 피드를 가리지 않고 묶인 모든 객체를 `calc_object`에 이유 "식별"로 쓰고, 그 표는 객체마다 한 줄(`ON CONFLICT DO NOTHING`)이라 조건 1만 고치면 판정은 후보인데 줄을 쓸 수 없다. 그래서 `old` 규칙이 식별이 넣은 객체도 **한 번** 판정한다. 처음 그 객체를 만난 단위가 `calc_xkey`에 새 종류(7, 식별이 넣은 객체)의 키를 적고 적은 키만 판정해서(ADR-0118 결정 5와 같은 방법) 여러 단위가 같은 객체를 다시 읽지 않는다. 후보이면 그 줄의 이유를 "퇴역 후보"로 바꾼다(`UPDATE`). 4단계는 퇴역 후보를 계산하지 않으므로(`why <> 퇴역 후보`) 퇴역할 객체에 값을 만들지 않는다. 바꾼 객체는 `Retiring`과 `OldObjects`에 센다(그래서 `calc_object`에서 이유가 "식별"이 아닌 줄의 수는 지금처럼 `OldObjects + ClaimObjects`다). 4단계(#110, #112)는 이유 "식별"을 따로 읽는 곳이 없고 "퇴역 후보가 아님"과 퇴역 후보의 수만 읽는다(`recompute_sql.go`, `recompute.go`). 식별이 짝을 남긴 실행은 지금처럼 후보가 없고 이유를 바꾸지 않는다. 이미 퇴역한 객체를 이 실행의 레코드가 아직 가리키면(값만 더하는 레코드가 퇴역한 객체에 다시 묶인 경우 등) 그 객체도 후보가 되어 4단계가 계산하지 않고 퇴역한 채 남지만, 이 실행이 퇴역시키는 것이 아니므로 `Retiring`에는 세지 않는다(`object.retired_seq`가 있는 객체를 뺀다). 검토자의 재현을 옮기자 퇴역 다음 실행부터 같은 객체를 다시 세어 옛 `Runner`와 수가 달랐다. 4단계의 시작이 세는 퇴역 후보의 수(`RecomputeCounts.Retiring`, "줄을 만들지 않는 객체")는 바꾸지 않았다.
3. **점진 확장도 피드 표를 받는다(G-3).** ADR-0118은 전체 계산만 표를 요구했다. 표가 없으면 모든 참조가 "표에 없는 피드"가 되어 받침이 되므로, 호출자(I-7)가 점진 확장에 표를 빠뜨리면 결정 1의 고침이 소리 없이 되돌아온다. 실행은 소스의 계획으로 레코드를 읽고 계획에는 피드가 있으므로(피드가 없는 매핑은 거절된다) 표가 빈 것이 맞는 확장은 없다. 그래서 `ExpandStart.Validate`가 모든 확장에 표를 요구한다.
4. **식별이 질문 표를 분석한다(G-4).** 식별은 실행이 채운 작업 테이블을 국면이 바뀔 때 `ANALYZE`하는데(ADR-0108 결정 7) 질문 표 `calc_ask`는 빠져 있었다. 통계가 없으면 질문을 읽는 문장(`read the questions`)의 계획이 운영 크기에서 질문 전체를 읽고 레코드를 하나씩 붙인 뒤 정렬한다(검토의 측정: 레코드 100만, 질문 60만에서 페이지 하나에 약 2초, 통계가 있으면 1,000개짜리 페이지에 2밀리초 이하). 그래서 한 차례의 질문을 다 쓰고 묻는 국면으로 넘어갈 때 `calc_ask`를 분석한다. 이렇게 하면 테스트 크기(레코드 수천)에서는 통계가 해시 조인과 `calc_record` 통째 읽기를 고르게 해서 식별의 계획 테스트가 결정적으로 깨진다(검토자의 흔들림의 원인이 이것이었다: 자동 분석이 끼면 깨졌다). 계획 테스트는 측정하는 세이브포인트 안에서 해시 조인, 병합 조인, 통째 읽기를 꺼(`SET LOCAL enable_hashjoin = off`, `enable_mergejoin = off`, `enable_seqscan = off`) 운영 크기에서 이기는 계획을 재고, 임계값(1.5배, `40*size`)은 그대로다. 해시 조인만 끄면 계획이 병합 조인으로 바뀌어 `calc_record`의 인덱스를 처음부터 끝까지 읽었다(측정: 큰 원본 2,310행, 작은 원본 761행). 이 측정 도우미(`idnHook`)를 쓰는 다른 계획 테스트(같은 곳의 모양, 여러 객체의 그룹, 4단계의 공유)도 같은 계획을 잰다. 확장의 스케일 테스트가 이미 그렇게 잰다. 그 힌트가 운영을 속이지 않음은 다른 테스트가 지킨다: 실제 실행을 돌려 질문을 읽을 때마다 서버가 아는 `calc_ask`의 행 수가 그 차례의 질문 수 이상인지(자동 분석을 끈 채) 보고, 레코드 10만과 질문 5만을 더해 분석한 뒤 서버가 고른 계획이 한 페이지(50개)의 질문과 레코드를 인덱스로 읽는지(각 표에서 페이지 크기의 4배에 50을 더한 행 이하, 통째 읽기 없음) `EXPLAIN`으로 본다.
5. **DISTINCT 둘은 읽는 양을 지킨다(G-8).** ADR-0118 결정 6의 "네 문장에서 DISTINCT를 빼도 읽는 행이 같다"는 거짓이었다. 검토가 서버가 고르는 계획 그대로, 영구 표마다 무관한 10만 줄을 넣고 재자 `old`의 DISTINCT를 빼면 한 객체를 공유하는 레코드 수의 제곱으로 읽고(m이 3배일 때 9배), 한도 규칙의 객체에서 빼면 계획이 무관한 영구 표를 통째로 읽었다(비는 1.5배 정도라 비 기준 테스트로는 못 잡는다). 둘은 읽는 양을 지키는 약속이다. 그래서 서버의 계획을 건드리지 않는 스케일 테스트를 둔다. 영구 표마다 실행과 무관한 3만 줄을 넣고, 한 단위로 모든 레코드를 읽을 때 문장마다 읽은 행이 실행의 레코드 수의 3배 이하(절대 상한)이고 m(200)과 3m에서 3.4배 이하(비)인지 본다. `key_old`와 `claim`의 DISTINCT는 빼도 읽는 행이 같다(검토의 측정).
6. **세대 표기(G-5).** `object_retire`와 `calc_xkey`는 생성기 9다. REQ-951, 설계 문서, `ingest_tables.go`의 주석이 8이라고 적었던 것을 9로 고친다.

## 고친 것 (ADR-0118과 ADR-0108의 바뀌는 문장)

| 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| ADR-0118 결정 1 | 조건 2의 참조를 객체를 만드는 피드의 것으로 좁힌다 | 조건 1의 묶임도 객체를 만드는 피드의 것만 센다. 식별이 넣은 객체도 `old` 규칙이 한 번 판정한다 (결정 1, 2) |
| ADR-0118 결정 1 | 전체 계산인데 표가 없으면 `CodeInvalidValue` | 모든 확장에 표가 없으면 `CodeInvalidValue` (결정 3) |
| ADR-0118 결정 6, 표의 결정 6 행 | DISTINCT를 빼도 읽는 행이 같고 테스트로 지키는 약속에서 뺀다 | `old`와 한도 규칙의 객체의 DISTINCT는 읽는 양을 지키고, 서버의 계획 그대로 재는 테스트가 지킨다 (결정 5) |
| ADR-0118 결정 3 | `object_retire`(생성기 8에서 9) | 생성기 9 (결정 6) |
| ADR-0108 결정 7 | 국면이 바뀔 때(복사 뒤, 슬롯 뒤, 라벨 뒤) `ANALYZE` | 질문을 다 쓴 뒤 `calc_ask`도 분석한다. 계획 테스트는 운영 크기에서 이기는 계획을 재고, 서버가 고르는 계획은 다른 테스트가 본다 (결정 4) |

## 검토자의 재현과 옮긴 테스트

| 검토자의 재현 | 이 저장소의 테스트 | 지키는 것 |
|---|---|---|
| `TestZZ2ChurningValueOnlyRecordDoesNotKeepTheObject` | `TestExpAnObjectIsRetiredAsTheOldPipelineRetiresItWhenTheRecordOfAFeedThatAddsValuesChangesInEveryRun` | 결정 1, 2. 옛 `Runner`와 실행마다 같은 단계에서 퇴역(8번의 실행) |
| (없음, 예외 상황) | `TestExpAnObjectThatOnlyAChangedRecordOfAFeedThatAddsValuesIsBoundToIsACandidateWhoeverGoesOnWithTheExpansion` | 만드는 피드의 레코드가 다른 객체로 가고 값만 더하는 레코드만 바뀐 객체는 후보, 이어받은 서버가 같은 판정, 표에 없는 피드의 묶임은 받침, 식별이 짝을 남기면 후보 없음, 한 번만 판정 |
| (없음, 예외 상황) | `TestExpAnObjectThatARecordOfAFeedThatMakesObjectsStandsUnderBesideAChangedRecordOfAFeedThatAddsValuesIsNoCandidate` | 같은 실행에서 두 피드가 묶은 객체, 만드는 피드의 레코드가 실행 밖에 서 있는 객체는 후보가 아님 |
| `TestZZ3FlakeConditions`, `TestZZ3ReadTheQuestionsAtScale` | `TestIdnTheQuestionsOfARoundAreReadByTheirIndexWithTheStatisticsTheRunGivesThePlanner` | 결정 4 |
| `TestZZ2ScaleOperatingPlan` | `TestExpWhatTheStatementsReadInThePlanTheServerChoosesGoesWithTheRecordsAndNotWithTheRestOfTheKG` | 결정 5. 테스트 시간을 10초 안에 두려고 크기를 줄이고(m 200, 무관한 줄 3만, 한 단위) 절대 상한을 더했다 |

## 넘기는 것

- **G-2 영구 표지가 퇴역과 부활을 넘어 남는다.** 옛 흐름은 퇴역할 때 객체의 상태를 지워 퇴역시키지 않는 피드로 부활한 객체를 다시 퇴역시키지 않는다. 새 흐름은 `object_retire`의 표지가 남아 다시 퇴역시킨다. 지지가 전혀 없는 객체라 지우는 사고는 아니다. 적용 단계(I-7)가 객체를 퇴역시키는 같은 트랜잭션에서 그 객체의 `object_retire` 줄을 지운다. 그 변경이 `MarkRetirableObjects`의 "표지는 남는다"와 REQ-951을 함께 고친다.
- **G-6 진 링크의 계약 테스트.** 한도에서 진 링크의 시작 참조를 미해결로 남기는 계약의 테스트는 I-5b2가 둔다.
- **G-7 피드 번호가 바뀐 뒤의 낡음.** 피드 번호는 매핑 안의 순서이고 참조의 `def_no`는 옛 번호로 남는다. 매핑이 바뀐 뒤 다시 읽히지 않은 레코드의 참조는 "표에 없는 피드"로 받침이 되거나, 순서가 바뀌면 다른 피드를 가리킨다. "매핑이 바뀌면 모든 레코드를 다시 식별한다"는 I-7의 계약이 이것을 닫는다.

## 이유

- 값만 더하는 피드의 레코드가 객체를 붙잡지 않는다는 규칙(REQ-643)은 레코드가 바뀌었든 아니든 같아야 한다. 바뀐 레코드만 받침이 되면 결과가 소스의 갱신 빈도에 달린다(결정 1).
- 식별의 줄을 지우지 않고 이유만 바꾸면 한 객체가 한 줄이라는 표의 약속과 4단계가 읽는 방법이 그대로다(결정 2).
- 통계는 운영 크기에서 맞는 계획을 고르게 하는 수단이고, 테스트 크기의 계획은 운영의 계획이 아니다. 그래서 운영 크기에서 이기는 계획을 재고, 그 힌트가 맞는지는 서버가 실제 통계로 고르는 계획을 따로 본다(결정 4).

## 대안과 기각 사유

- **식별이 값만 더하는 피드의 묶임에는 `calc_object` 줄을 쓰지 않는다.** 4단계는 그 줄로 값을 다시 계산할 객체를 고른다. 만드는 피드의 레코드가 실행에 없고 값만 더하는 레코드만 바뀐 객체(흔한 경우)는 값을 다시 계산해야 하므로 줄이 있어야 한다.
- **`calc_object`에 컬럼을 더해 "판정함"을 적는다.** 스키마를 바꾸는 일이고(이 변경에서 하지 않는다), 같은 일을 이미 있는 `calc_xkey`의 새 종류가 한다.
- **조건 1을 `calc_bind`로 읽는다.** `calc_bind`에는 객체로 찾는 인덱스가 없다. 식별이 묶임마다 쓰는 `calc_ref`의 해결된 신원 참조는 같은 내용이고 `calc_ref_target` 인덱스가 있다.
- **계획 테스트의 원본을 운영 크기로 키운다.** 테스트 시간이 크다.

## 알려진 한계

- 식별이 넣은 객체마다 `old` 규칙이 조건 1을 한 번 읽는다(객체의 묶임을 인덱스로, 만드는 피드의 묶임을 하나 찾으면 멈춘다). 전체 계산에서는 모든 객체가 한 번씩이다.
- 질문 표의 분석은 차례마다 한 번이다. 한 차례의 답이 쌓이는 동안에는 다시 분석하지 않는다.

## 결과

- 확장에 피드 표를 주지 않는 호출은 점진 확장이어도 거절된다. 잠긴 테스트 중 표 없이 확장을 시작하던 것은 입력에 표만 더했다(아래).
- 스키마와 생성기 세대는 그대로다.

## 잠긴 테스트의 변경

프런트매터 `amends_tests`에 적은 테스트들이다. 건별 이유는 `test_change_reason`에 있다. 단언을 지우거나 약하게 하거나 임계값을 넓힌 것은 없다.
