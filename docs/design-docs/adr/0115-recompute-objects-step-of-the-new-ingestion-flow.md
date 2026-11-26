---
title: ADR-0115 새 수집 흐름의 객체 다시 계산(4단계)과 해시·인코딩의 공개
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/ingest/rcp_order_test.go
  - backend/internal/pgingest/rcp_parity_test.go
  - backend/internal/pgingest/rcp_full_test.go
  - backend/internal/pgingest/rcp_scale_test.go
  - backend/internal/pgingest/rcp_resume_test.go
  - backend/internal/pgingest/rcp_values_test.go
  - backend/internal/pgschema/rcp_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/design-docs/ingestion-at-scale.md
  - docs/product-specs/requirements-ingestion.md
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
test_change_reason: 생성기 버전이 7에서 8로 오른다(이 결정 10). 이 테스트는 생성기 버전의 번호를 단언하므로 숫자만 8로 바꾼다. 파티션 접두어를 예약한다는 단언은 그대로이고 줄지 않는다.
decision_hash: cb7e3e89eb849f88
amended_by:
  - adr: "0119"
    what: 건너뛴 객체의 규칙(4항, 8항), 한도의 값과 메모리 근거(8항), 이어받은 서버가 쓰는 시각과 체계(3항), 4단계가 가진 문제 종류(9항), 생성기 버전(10항)
  - adr: "0122"
    what: 4단계의 링크 쪽(범위), 4단계가 가진 문제 종류(9항), 생성기 버전(10항)
---
# ADR-0115 새 수집 흐름의 객체 다시 계산(4단계)과 해시·인코딩의 공개

## 맥락

ADR-0114까지가 3단계(식별)와 영향 집합의 확장이다. 이 결정은 4단계의 객체 쪽이다. `calc_object`에 든 객체를 영향받은 레코드와 서 있는 레코드 모두로 다시 계산해 `stage_object`, `stage_volatile`, `stage_ident`에 쓴다. 링크 쪽(`stage_link`, 끝 풀기, 차수)은 I-5b2의 몫이다. 옛 수집(`ingest.Runner`)은 그대로 두고 비교의 기준으로만 쓴다. 값을 고르고 해시를 만드는 일은 SQL이 아니라 Go가 한다(`ingestion-at-scale.md` P3). 데이터베이스는 어느 레코드가 어느 객체 밑에 서 있는지 읽어 주고 Go가 만든 줄을 쓸 뿐이다.

## 결정

1. **해시와 인코딩은 `core.graph`가 공개한다.** 행 단위 경로(`Applier`)와 새 흐름이 같은 뜻을 쓰도록 `graph.ContentHasher`(`NewContentHasher`, `Object`, `Link`), `graph.MinGrade`, `graph.GradeNo`, `graph.EncodeAttrs`, `EncodeRefs`, `EncodeKey`, `DecodeKey`, `EncodeVolatileEntry`를 공개했다. `pgstore`의 같은 이름의 함수는 이것을 부르는 한 줄이다. 동작은 바뀌지 않는다(기존 `sap_*`, `store_*` 테스트가 그대로 통과한다). 링크 내용 해시(`ContentHasher.Link`)도 같은 파일에 있으나 쓰는 곳은 I-5b2다.

2. **포트는 `core.ingest`의 `Recomputer`이고 구현은 `adapter.pgingest`다.** `ObjectMaker`(Go)가 한 페이지를 받아 줄을 만든다. 모든 호출이 임대의 시도 번호를 대고 읽기도 그렇다. 시도가 다르면 `CodeRunLeaseLost`로 거절하고 아무것도 바꾸지 않는다.

   | 메서드 | 하는 일 |
   |---|---|
   | `BeginRecompute(lease, start)` | 확장이 끝난 실행만 받는다. `calc_job`을 4(`CalcJobRecompute`)로 바꾸고 자리와 폐기 후보 수를 기록한다. 둘째 호출과 확장이 끝나지 않은 실행은 `CodeRunStage` |
   | `RecomputePlace(lease)` | 시작했는지, 끝났는지, 몇 페이지를 했는지, 어느 객체까지 했는지 |
   | `RecomputePage(lease)` | 자리가 가리키는 페이지를 읽는다(아무것도 바꾸지 않는다) |
   | `PutRecomputed(lease, page)` | Go가 만든 줄을 쓰고 자리를 옮긴다. 한 트랜잭션이고, 단위 번호가 자리와 다르면(같은 페이지를 두 번 보냄) `CodeRunStage`로 거절하고 아무것도 세지 않는다 |
   | `RecomputeReport(lease)` | 센 수 |

   `RunRecompute`가 위를 이어 돌린다. 시작은 한 페이지의 줄 수(`Lines`, 기본 `DefaultRecomputeLines`), 한 객체의 줄 수 한도(`ObjectMax`, 기본 `DefaultRecomputeObjectMax`), 값을 본 시각(`Seen`, 실행의 시각, Z로 끝나는 UTC), 식별자 체계의 번호를 준다. 시각을 실행이 정해 주므로 이어받은 서버가 같은 문서를 쓴다.

3. **한 객체의 줄은 그 객체 밑에 서 있는 열린 레코드와 피드의 쌍이다.** 영구 `record_ref`의 식별 참조 중 이번 실행이 다시 읽어 참조를 새로 쓴 레코드의 것(`expReplaced`, ADR-0114가 퇴역 후보를 판단하는 그 식)을 뺀 것과 이번 `calc_ref`다. 객체는 번호 순이고 페이지는 줄 수로 자른다(한 객체가 한도 안에서 페이지 하나보다 크면 그 객체가 한 페이지다). 이번 실행의 레코드가 아닌 레코드는 페이지를 쓸 때 이유 `expanded`로 이번 실행의 레코드가 된다(`Added`로 센다). 세 번째 단계 이후가 모든 레코드를 한 집합으로 보게 하려는 것이다. 폐기 후보(`calc_object.why` = 4)는 줄을 만들지 않는다. 후보의 판단은 `expRetirable`만 하고 여기서 다시 하지 않는다.

4. **레코드는 그 소스의 계획으로 읽는다.** 같은 선언에서 만든 레코드만 읽는다. 다른 선언에서 만든 것이거나 소스의 계획이 없거나 셀 수가 선언과 다르면 그 객체는 읽을 수 없는 객체(`Unreadable`)이고 이번 실행에서 그대로 둔다. 객체의 일부 레코드만으로 객체를 만들지 않는다.

5. **속성마다 이기는 값은 전순서의 첫째다.** 순위(`rank`) 오름, `opened_seq` 내림(나중에 열린 레코드), 소스 번호 오름, 피드 번호 오름, 데이터셋 번호 오름, 자연 키의 바이트 오름, 컬럼 이름 오름이다. 같은 레코드, 피드, 컬럼의 값만 같아 다른 두 값은 늘 순서가 있다. 그래서 줄을 읽은 순서와 페이지의 크기가 결과에 영향을 주지 않는다(`TestRcpTheOrderOfTheValuesIsAStrictTotalOrder`, `TestRcpTheValueChosenDoesNotDependOnTheOrderTheLinesAreRead`, `TestRcpThePageSizeDoesNotChangeWhatIsStaged`). 링크의 중복은 뒤가 이긴다(I-5b2). 방향이 반대인 것을 일부러 적어 둔다. 소스 사이는 소스 번호 오름이다(소스 사이의 우선순위가 생기기 전까지 잠정).

6. **식별자는 체계마다 값 하나다.** 객체가 가진 것으로 판정된 주장(`record_ref`의 주장 참조 중 해결된 것)의 값 중 `opened_seq` 내림, 자연 키 오름, 소스 번호, 피드 번호, 값 순으로 첫째다. 값은 정규화된 값이다.

7. **이름.** 이긴 `name` 값이 있으면 그것이다. 없으면 객체가 처음 보인 키(`first_key`)의 문맥이 아닌 첫 값이고, 키가 없는 객체(`first_key`가 없는 기존 객체)는 등록부의 기존 이름을 그대로 둔다. 새 객체는 식별이 이름을 확인했으므로 이름이 있다. 새 객체의 `first_key`는 그 객체를 만든 레코드와 피드(`calc_newobj`)에서 후보 id와 같은 id를 내는 키로 다시 만든다. 문맥이 있는 키는 질문(`calc_ask`)의 부모 id를 쓴다.

8. **한도.** 한 객체의 줄이 `DefaultRecomputeObjectMax`(100,000)를 넘으면 그 객체는 이번 실행에서 건너뛰고(`TooMany`, `Left`) 줄을 쓰지 않으며 폐기하거나 바꾸지 않는다. 근거는 메모리다. 한 페이지는 한 객체의 줄을 모두 담으므로 이 수가 페이지의 메모리를 묶는다(레코드 10만 개의 셀은 수십 MB). 그만큼의 레코드가 한 객체 밑에 서는 것은 의도한 쓰임이 아니라 매핑이 너무 많은 행으로 객체 하나를 만든 경우이므로, 객체의 일부로 만들지 않고 보고에 센다(I-7이 경고로 드러낸다). 한 페이지의 줄 수 기본값 `DefaultRecomputeLines`는 10,000이고 한도는 페이지보다 크다(테스트으로 고정).

9. **문제.**
   - `source_conflict`는 4단계가 갖는다. 같은 속성에 서로 다른 글을 준 레코드의 줄 중 진 줄마다 그 레코드의 문제를 낸다. 이긴 줄과 같은 글이면 문제가 아니다.
   - 객체를 만들지 않고 값만 붙이는 피드의 읽을 수 없는 셀은 `invalid_value`다. 수신이 객체를 만드는 피드에 대해 내는 것과 한 쌍이다. 지금 받은 레코드(바뀜, 새로움, 되돌아옴)만 낸다.
   - 링크 쪽의 `link_target`, `link_ambiguous`, `cardinality`는 `StepFourProblemKinds`가 이름을 말하지만 이 결정이 내지는 않는다(I-5b2). `link_conflict`와 고정 목록의 CHECK를 넓히는 일도 I-5b2다.

10. **생성기를 8로 올린다.** `calc_ref (role, target_no)`와 `calc_ask (rid, feed_no)`의 부분 인덱스를 더한다(`recomputeSteps`, 두 번 해도 같고 표가 없는 스키마는 그대로 둔다). `calc_bind`에는 객체 번호의 인덱스가 필요하지 않아 더하지 않는다. 번호는 병합 순서에 따라 나중에 다시 매길 수 있다.

11. **읽는 양은 줄 수에 비례한다.** 객체마다 줄은 `record_ref_target`과 `calc_ref_target`으로 찾고 주장은 레코드의 기본키 앞부분과 `calc_ref_pair`로 찾는다. 테스트(`TestRcpWhatManyRecordsOfAnObjectShareIsReadOnceSoTheRowsReadGoWithTheNumberOfLines`)은 150개와 450개 객체에서 줄을 읽는 문장이 읽은 행이 1,351과 4,051(3.0배)임을 보인다.

## 옛 흐름과 다른 점

테스트(`TestRcpTheStagedObjectsAreTheOnesTheOldPipelineMakesFromTheSameCaptures`)은 옛 `Runner`의 일곱 실행(`idn_parity_more`의 입력)에서 새 흐름이 낸 줄을 옛 `graph.ObjectRow`와 이름, 최소 등급, 속성 문서(시각 제외), 출처 목록, 내용 해시로 견준다. 다른 곳은 표의 항목뿐이다.

| 다른 점 | 옛 | 새 | 이유 |
|---|---|---|---|
| 1 값을 본 시각(`t`) | 값이 바뀌지 않으면 처음 본 시각이 남는다 | 다시 계산한 실행의 시각(`Seen`) | 해시에 들지 않는다(내용 해시는 같다). 새 흐름은 지난 값을 읽지 않고 레코드에서 다시 만든다 |
| 2 이기는 값의 순서 | 소스마다 실행하고 한 소스 안에서 나중 행이 이긴다 | 위 결정 5의 전순서. 소스 사이는 소스 번호 오름 | 옛 `Runner`는 소스 하나씩이라 소스 사이의 우선순위가 없다 |
| 3 식별자 | 체계마다 하나, 나중에 본 값이 이긴다(처리 순서) | 체계마다 하나, `opened_seq` 내림과 자연 키 오름(결정 6) | 처리 순서가 아니라 레코드의 성질로 정해 이어받아도 같다 |
| 4 `first_key` | 객체를 만든 실행이 메모리에서 쓴다 | 객체를 만든 레코드의 키로 다시 만든다 | 이어받은 서버도 같은 값을 만든다 |
| 5 `source_conflict` | 한 실행의 메모리 안에서 비교 | 줄마다의 전순서 비교(진 줄 하나마다 문제 하나) | 이어 가도 같은 문제를 낸다. 테스트는 옛 흐름의 문제와 견주지 않고 손으로 계산한 사례로 고정한다(`TestRcpTwoRecordsOfOneObjectThatSayDifferentThingsHaveOneValueAndTheLosingOneIsAProblem`) |

## 이유

- 값을 고르는 일을 총순서로 두면 페이지의 크기, 읽은 순서, 중단 뒤 이어서 하기와 상관없이 같은 결과를 낸다는 것을 테스트으로 보일 수 있다.
- 줄을 레코드에서 다시 읽으면 "이번에 건드린 객체"와 "건드리지 않은 객체"의 계산이 같은 코드다. 건드린 객체의 줄은 건드리지 않은 것을 포함해 전체 계산(`ExpandStart.Full`)의 줄과 같고, 건드리지 않은 객체는 전 실행의 줄과 같다(`TestRcpTheObjectsTheRunTouchedAreTheSameWhenEveryRecordIsComputedAndTheOthersAreWhatTheRunBeforeMade`).
- 규칙 (다)에서 (바) 중 객체의 줄을 바꾸는 것은 식별의 결과(새 객체)와 (바)의 주장이다. (라)와 (마)는 링크의 레코드를 이번 실행의 레코드로 더할 뿐이라 이 결정의 테스트에서는 객체의 줄을 바꾸는 사례가 없다(그 효과는 I-5b2의 링크가 테스트한다). 퇴역 후보(`old` 규칙)는 줄을 만들지 않으므로 이 결정의 테스트으로는 `old` 규칙을 건너뛰어도 객체의 줄이 달라지지 않는다.

## 대안과 기각 사유

- **페이지를 객체 수로 자른다.** 한 객체가 레코드 수만 개를 가질 수 있어 페이지의 메모리를 묶을 수 없다.
- **값을 SQL로 고른다.** 순위와 열린 순서까지는 SQL로 되지만 셀의 변환과 검사가 Go의 순수 함수에 있어 두 곳에 같은 뜻이 생긴다.
- **`source_conflict`를 다른 종류로 접는다.** 뜻이 다르다(결정 6의 기각과 같다).

## 알려진 한계

- 한 레코드가 두 객체 밑에 서면(다른 피드) 문제를 객체마다 낸다. 같은 (레코드, 피드, 컬럼)의 문제가 두 번 나지는 않는다.
- 값을 건너뛴 객체(`TooMany`, `Unreadable`)는 `Left`로 센다. 사용자에게 드러내는 일은 I-7이다.
- 필드가 없는 기존 객체의 `first_key`는 읽을 수 없어 이름 규칙이 등록부의 이름을 그대로 둔다.
- 값을 본 시각이 실행마다 바뀌어 문서(`doc`)가 바뀌어도 내용 해시는 같다. 차이(5단계)는 해시로 가르므로 문서만 바뀐 객체를 바뀐 것으로 세지 않는다.

## 결과

- 5단계(차이)는 `stage_object`, `stage_volatile`, `stage_ident`를 읽으면 된다. 폐기 후보는 `calc_object`의 이유 4다.
- 상태 기계(I-7)는 `RunExpand` 다음에 `RunRecompute`를 부른다.
- 생성기가 8이다. 번호는 병합 순서에 따라 다시 매긴다.
