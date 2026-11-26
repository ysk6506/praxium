---
title: ADR-0128 새 수집 흐름 9단계 정리와 적용 쪽 이음(표지), 실행 상태 읽기
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/stl_settle_test.go
  - backend/internal/pgingest/stl_rules_test.go
  - backend/internal/pgingest/stl_flow_test.go
  - backend/internal/pgingest/stl_resume_test.go
  - backend/internal/pgingest/stl_scale_test.go
  - backend/internal/pgingest/stl_state_test.go
  - backend/internal/pgingest/stl_frozen_test.go
  - backend/internal/pgschema/problemdef_test.go
  - backend/internal/pgstore/stm_mark_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0095-ingestion-tables-in-the-schema-plan.md
  - docs/design-docs/adr/0118-expansion-follow-up-support-mark-and-units-after-review.md
  - docs/design-docs/adr/0122-recompute-links-step-of-the-new-ingestion-flow.md
updates_docs:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/ingestion-at-scale.md
amends:
  - adr: "0095"
    what: 결정 3의 record_problem·calc_problem의 def_no 뜻(링크 정의는 -(n+1))
  - adr: "0122"
    what: 링크 쪽 문제가 남기는 정의 번호(링크 정의는 -(n+1))
  - adr: "0118"
    what: 결정 3의 "표지는 한 번 쓰면 남는다"(적용 청크가 달고 퇴역 때 지운다)
amends_tests:
  - go:backend/internal/pgingest::TestRclTheLinkOfADuplicateIsMadeByTheRecordThatComesLastACellThatGaveNoValueIsSaidAndAHeldRecordOrABlankKeyIsNoProblem
test_change_reason: 링크 정의의 문제가 남기는 정의 번호가 0에서 -1로 바뀐다(이 결정 1). 이 시험은 링크 칸의 invalid_value 줄을 키/종류/정의 번호/필드로 견주므로 기대 글의 번호만 0에서 -1로 바꾼다. 견주는 줄의 수와 다른 칸은 그대로이고 단언은 약해지지 않았다.
decision_hash: 4b0074523351ee4c
---
# ADR-0128 새 수집 흐름 9단계 정리와 적용 쪽 이음(표지), 실행 상태 읽기

## 맥락

새 흐름의 단계 1~5(앞부분)와 7·8은 들어와 있다. 9단계(정리)는 제품 코드가 없고, 시험 도우미 `rdfSettle`만 ②③과 `duplicate_key`를 흉내 냈다. [수집의 규모 설계](../ingestion-at-scale.md) 4절의 표는 정리를 ① 확정 ② `raw_record` ③ `record_wait` ④ `record_ref`·`record_problem` ⑤ `source_state` ⑥ 준비 테이블 `TRUNCATE` ⑦ 통계 ⑧ 실행 기록 끝으로 적고, 구간마다 "준비 테이블의 값으로 덮어쓰기"라 다시 해도 같다고 한다. 장부에서 넘겨받은 일은 셋이다. 퇴역 표지(`object_retire`)를 적용 쪽이 쓰고 퇴역 때 지우는 것(F-3, G-2, ADR-0118, ADR-0121), 한 레코드가 두 객체 밑에 서고 한 객체만 다시 계산될 때 다른 객체의 문제가 남는지(F-6, ADR-0119), 레코드의 운명을 정하는 것이다.

코드를 읽어 확인한 사실 둘이 설계를 정했다. 문제의 `def_no`는 피드 번호(매핑의 feeds 순서, `ingest/compile.go`)와 링크 정의 번호(links 순서, `ingest/compile_link.go`)가 둘 다 0부터 같은 공간을 썼다. `source_conflict`와 `invalid_value`는 두 쪽 모두에서 나서, 확장된 레코드가 피드 k와 링크 정의 k를 함께 가지면 (레코드, 정의)로 어느 줄을 갈아 끼울지 가를 수 없었다. 그리고 수신은 원본 레코드의 운명이 걸러짐이나 안 쓰임이면 그 행이 지난번에 걸러졌다고 보고 바뀜을 정한다(`pgingest/sink.go`의 `decide`, 잠긴 시험 `TestPgiARecordIsNewChangedOrTheSameAndOnlyNewAndChangedOnesCarryCellsKeysEndsAndClaims`).

## 결정

1. **문제의 정의 번호는 피드와 링크 정의를 부호로 나눈다.** 피드 n은 n, 링크 정의 n은 -(n+1), 레코드 전체의 문제(`duplicate_key`)는 NULL이다. 만들고 푸는 함수(`pgschema.ProblemDefOfFeed`, `ProblemDefOfLink`, `ProblemDefRead`)와 SQL 식(`ProblemDefFeedSQL`, `ProblemDefLinkSQL`, `ProblemDefIsFeedSQL`, `ProblemDefIsLinkSQL`)은 `pgschema` 한 곳에 있고, 문제를 쓰는 곳 넷(식별, 4단계 객체, 4단계 링크, 격리)이 모두 이것을 쓴다. 생성기 세대는 그대로다(열을 더하지 않는다).
2. **포트는 `core.ingest`의 `Settler`(`BeginSettle`, `SettlePlace`, `SettleBucket`)이고 `RunSettle`이 몬다.** 구현은 `adapter.pgingest`다. 실행 기록의 `settle_job`이 일, `settle_part`가 그 일의 끝난 구간 수, 카운트의 `settle`이 일 안의 자리와 센 수다. 구간, 자리 이동, 임대 연장이 한 트랜잭션이라 구간은 한 번만 된다. 자리가 아닌 구간(끝난 구간, 앞선 구간)은 `CodeRunStage`, 임대를 잃은 서버는 읽기도 `CodeRunLeaseLost`다. 시작은 `settling` 단계이고 5단계(격리)가 끝났고 계산 테이블이 그 실행의 것일 때만이다. 정리를 다시 시작하면 `CodeRunStage`다.
3. **일의 순서는 ① → ④ → ② → ③ → ⑤ → ⑥ → ⑦ → ⑧이다.** 운명은 ④가 쓴 줄을 읽어야 하므로 ④가 ② 앞이다. 레코드를 도는 일(④의 앞부분, ②)은 `calc_record`의 번호 범위(`Rows`, 기본 `DefaultSettleRows` 10,000)이고, 레코드 색인이 없는 `calc_problem`과 `calc_wait`은 블록 범위(TID 범위)로 읽는다. 한 구간의 문장은 레코드마다 색인으로 줄을 찾는다(측면 하위 질의와 한 줄에서 멈추는 조건). 원본 레코드의 운명을 쓰는 문장은 그 트랜잭션에서 해시·병합 조인을 끈다.
4. **④ 참조와 문제는 이번 실행이 다시 계산한 (레코드, 정의) 쌍의 줄만 갈아 끼운다.** "다시 읽음"(replaced)은 확장의 `expReplaced`(ADR-0114)와 같은 판단에 닫힘을 더한 것이다. 받은 레코드는 `why`가 새로 옴·바뀜이다.

   | `why` | `record_ref` | `record_problem`에서 지우는 줄 | 넣는 줄 |
   |---|---|---|---|
   | 새로 옴(3), 바뀜(0) | 모두 갈아 끼움 | 모두 | `calc_problem` + 수신이 그 행에 남긴 문제(`stage_record.problems`, 피드 ID를 시작 값의 피드 표로 번호로) |
   | 닫힘(5) | 모두 지움 | 모두 | 없음 |
   | 놓아줌(6), 그리고 미해결(1)·돌아옴(4)·반복 바뀜(7) 중 선언 개정이 데이터셋의 지금 개정과 같은 것(다시 읽음) | 모두 갈아 끼움 | 링크 정의의 줄 모두. 피드의 줄 중 식별·4단계·5단계 종류(`source_conflict`, `identity_conflict`, `value_without_object`, `missing_name`, `context_parent_missing`, `shape_changed`). 돌아옴은 값만 붙이는 피드의 `invalid_value`도 | `calc_problem` |
   | 확장(2), 그리고 선언 개정이 다른 미해결·돌아옴·반복 바뀜(식별이 비켜 둠) | `calc_ref`에 있는 (역할, 정의) 쌍만 갈아 끼움 | 링크 정의 중 이번 `calc_end`에 끝이 있는 것의 줄. 피드 중 그 피드의 식별 참조가 이번에 계산한 객체(`stage_object`)를 가리키는 것의 `source_conflict`, `identity_conflict` | `calc_problem` |

   돌아옴·놓아줌·반복 바뀜은 원본 차이가 키의 반복을 다시 판단했으므로 `duplicate_key`(정의 없음)도 지우고 `calc_problem`의 것을 넣는다. 미해결과 확장은 그대로 둔다(`rdfSettle`과 같다).

   수신이 남긴 종류(`missing_identity`, 객체를 만드는 피드의 칸)는 받지 않은 레코드에서 그대로 남는다. 한 레코드가 두 객체 밑에 서고 한 객체만 계산되면(F-6) 다른 피드의 문제는 남고, 피드 k와 링크 정의 k는 부호로 갈린다(`TestStlARecordUnderTwoObjectsOfWhichOneIsComputedKeepsTheLinesOfTheOtherAndAFeedAndALinkDefinitionOfOneNumberAreApart`).
5. **② 레코드의 운명은 ④ 뒤의 줄에서 SQL로 정한다.**

   | 순서 | 조건 | 운명 |
   |---|---|---|
   | 1 | 걸러진 행(`calc_record.fate`가 걸러짐: 받은 레코드는 수신의 표시, 나머지는 지난 운명) | 걸러짐 |
   | 2 | 해결된 식별 줄이나 해결된 링크 From 줄이 있음 | 쓰임 |
   | 3 | 문제 줄이 있음 | 문제 |
   | 4 | 그 밖 | 안 쓰임 |

   걸러짐이 앞인 것은 수신의 `decide` 때문이다. 걸러진 행에 쓰임을 주면 같은 행이 실행마다 바뀜으로 읽혀 `opened_seq`가 오르고 "나중 값이 이김"이 흔들린다. 해결된 링크 From 줄을 쓰임으로 보는 것은 출력이 그대로인 레코드를 다음 실행이 다시 읽지 않게 하려는 것이다(`TestStlARecordThatOnlyMakesALinkIsUsedAndTheNextRunOfItsSourceDoesNotReadItAgain`). 닫힌 레코드는 지우고, 받은 레코드는 칸과 함께 덮어쓰고, 나머지는 운명만 쓴다.
6. **③ 기다림은 그 소스의 `record_wait`을 키 범위로 지운 뒤 `calc_wait`을 블록 범위로 넣는다.** 원본 차이가 그 소스의 남는 기다림을 모두(세지 않은 것까지) `calc_wait`에 썼으므로 갈아 끼움이다. 안 보임의 기다림이 끝나 닫힌 줄은 넣지 않는다(`rdfSettle`과 같다). 불완전한 데이터셋은 원본 차이가 줄을 그대로 옮겨 두었으므로 그대로다.
7. **⑤ 소스는 적용한 정의 세트(`applied_definition_set` = 실행의 `definition_set_hash`)를 쓰고, 완전히 읽은 데이터셋의 `captured_at`을 실행의 시각(`counts.raw_diff.at`)으로 쓴다.** 캡처 해시는 쓰지 않는다. 새 흐름의 수신은 페이지로 읽어 해시를 만들지 않는다. 선언 개정(`decl_rev`)은 선언이 바뀔 때 쓰는 값이라 정리가 손대지 않는다. "전체 다시 수신"·"전체 계산" 표시는 I-7c의 일이다.
8. **⑥은 계산 테이블과 그 소스의 수신 파티션을 한 트랜잭션에서 비운다. ⑦은 정리가 쓴 영구 테이블 넷(소스의 `raw_record` 파티션, `record_ref`, `record_problem`, `record_wait`)을 구간마다 하나씩 `ANALYZE`한다. ⑧은 실행을 `succeeded`로 끝낸다(`EndRun`과 같은 문장을 그 구간의 트랜잭션에서).** 다른 소스의 수신은 자기 파티션에 쓰고, 같은 KG의 다른 실행은 관문 때문에 정리 중에 계산 단계로 가지 못한다(`TestStlTheStagedRowsOfAnotherSourceAreNotEmptiedAndItsRunWaitsAtTheGateWhileOneIsCleaned`).
9. **① 확정은 그래프의 일이고 `ingest.Confirmer`가 한다.** `RunSettle`은 자리가 ①이면 `Confirm`을 부른 뒤 그 구간을 쓴다. 그 사이에 끊기면 다시 부르므로 두 번 불러도 같아야 한다. 제품의 구현은 `ingest.SealConfirmer`이고 옛 `Runner.apply`의 `Seal(SealSync)`과 같다(그래프가 바뀌지 않았으면 버전을 만들지 않는다). KG 임대를 가진 상태 기계(I-7b)가 만들어 준다.
10. **표지(F-3, G-2).** 표지 문장은 `pgschema`(`MarkRetirableSQL`, `MarkRetirableChunkSQL`, `UnmarkRetiredChunkSQL`)에 두고 `pgingest`와 `pgstore`가 함께 쓴다. `pgstore`의 적용 청크는 등록부를 쓴 뒤 그 청크의 객체 중 이번 실행의 `retirable` 식별 참조가 가리키는 객체에 표지를 달고, 이어서 그 청크가 퇴역시키는 객체의 표지를 지운다. 같은 트랜잭션이고 표지가 먼저, 지우기가 나중이다. 퇴역한 뒤 퇴역시키지 않는 피드로 되살아난 객체는 표지가 없어 남은 표지 때문에 다시 퇴역하지 않는다.
11. **실행 상태 읽기 `ingest.RunStateReader`(`RunState`).** 단계, `calc_job`, `calc_part`, 계산 테이블이 그 실행의 것인지, `settle_job`, `settle_part`, 카운트로 본 단계별 끝남(수신, 원본 차이, 식별, 확장, 객체, 링크, 격리)을 준다. 계산 단계의 끝남은 계산 테이블이 그 실행의 것일 때만 참이다. 임대 없이 읽는다.

## 이유

- 열을 더하면 기존 표 `record_problem`의 `ALTER`라서 읽는 쪽이 연 표를 건드리지 않는다는 약속(REQ-680)과 맞지 않는다. 부호는 같은 정보를 열 없이 담는다.
- 정리를 레코드 번호와 블록 범위로 나누면 한 구간이 읽는 행은 그 구간의 레코드에 비례하고 표의 크기와 상관없다(레코드 m과 3m에서 같다, `TestStlABucketOfTheCleaningReadsTheLinesOfItsRecordsAndNotTheRestOfTheTablesSoWhatItReadsDoesNotGrowWithTheSource`).
- 운명을 ④ 뒤에 정하면 다시 계산하지 않은 쌍의 남은 줄까지 본다. 계산 테이블만 보면 확장된 레코드의 남은 문제를 놓친다.

## 대안과 기각 사유

- **`record_problem`에 정의 종류 열을 더한다(세대 15).** REQ-680과 맞지 않는다(설계 책임자 결정).
- **사유 글과 `field`로 피드와 링크를 가른다.** 글에 기대는 판단이라 기각.
- **낡은 줄을 다음 전체 계산이 치운다.** 설계 책임자 결정 7에서 기각.
- **운명에서 쓰임을 걸러짐보다 앞에 둔다(옛 흐름의 세는 순서).** 수신의 바뀜 판단과 맞지 않아 기각(결정 5).
- **`calc_problem`에 레코드 색인을 더한다.** 생성기 세대가 오른다. 블록 범위로 읽으면 필요 없다.

## 알려진 한계

- 어떤 피드도 링크 정의도 읽지 않는 데이터셋의 행은 안 쓰임이고, 수신이 안 쓰임을 걸러짐으로 보는 잠긴 동작 때문에 실행마다 바뀜으로 다시 읽힌다. 링크만 만드는 행은 해결된 From 줄로 쓰임이라 이 경우가 아니다.
- 다시 읽은 레코드가 건너뛴 기존 객체(`TooMany`, `Unreadable`) 밑에 서면 그 피드의 옛 `source_conflict`는 지워지고 4단계가 쓴 건너뜀의 문제만 남는다.
- 적용(I-5c2의 차이 뒤)이 아직 정리 앞에 이어지지 않아, 시험은 그래프 쪽을 시험 도우미(`stlGraph`)로 대신한다. 정리 자체는 그래프를 읽지 않는다(②의 운명과 ④는 계산 테이블과 영구 레코드 표만 읽는다).
- 정리의 시작은 지금 5단계 앞부분(격리)이 끝났는지를 본다. 차이(I-5c2)가 들어오면 그 끝남도 함께 봐야 한다.

## 결과

- 정리 뒤 미해결 목록은 다음 실행의 출발 레코드이고(ADR-0114), 레코드 참조는 KG에 없는 객체나 링크를 가리키지 않는다(`TestStlAfterTheCleaningTheRecordsNotSolvedAreTheStartOfTheNextRunAndNoReferencePointsAtWhatTheKGHasNot`).
- 정리까지 이어진 실행이 남기는 살아 있는 객체와 링크는 옛 `Runner`와 같고, 그 수와 id 해시를 시험에 얼려 두었다(`TestStlWhatLivesAfterARunCleanedToTheEndIsWhatTheOldPipelineLeavesAndIsFrozen`). I-7d에서 옛 길을 지워도 이 기대값이 남는다.
- `rdfSettle`과 그것을 부르는 시험은 그대로다. 같은 실행을 두 벌로 만들어 한쪽은 `rdfSettle`, 한쪽은 정리로 끝내면 `raw_record`(운명 빼고), `record_wait`, `duplicate_key` 줄이 같다(`TestStlTheCleaningLeavesTheRawRecordsTheWaitsAndTheRepeatedKeysTheTestCleaningOfTheComparisonLeft`).
- 요구사항 REQ-984~988. REQ-951의 "표지는 남는다"를 고친다.
