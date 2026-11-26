---
title: ADR-0122 새 수집 흐름 4단계의 링크 쪽 — 끝 풀기, 링크 번호·id·속성·해시, 같은 링크의 승자, 쪽과 이어 가기
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/rcl_elect_test.go
  - backend/internal/ingest/rcl_make_test.go
  - backend/internal/pgingest/rcl_parity_test.go
  - backend/internal/pgingest/rcl_scale_test.go
  - backend/internal/pgingest/rcl_order_test.go
  - backend/internal/pgingest/rcl_edge_test.go
  - backend/internal/pgingest/rcl_ends_test.go
  - backend/internal/pgschema/rcl_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0115-recompute-objects-step-of-the-new-ingestion-flow.md
updates_docs:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/ingestion-at-scale.md
amends:
  - adr: "0115"
    what: 4단계의 링크 쪽(범위), 4단계가 가진 문제 종류(9항), 생성기 버전(10항)
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgschema::TestRcpTheKindsOfProblemTheFourthStepOwnsAreOnlyTheOnesItWritesAndEveryOneOfThemIsInTheClosedList
test_change_reason: 단언을 줄이거나 임계값을 넓힌 것은 없다. (1) TestControlReservesThePartitionPrefix는 생성기 버전의 숫자를 단언하므로 11에서 12로 바꾼다(이 결정 9). (2) TestRcpTheKindsOfProblemTheFourthStepOwnsAreOnlyTheOnesItWritesAndEveryOneOfThemIsInTheClosedList는 4단계가 가진 종류의 목록을 정확히 견주는 단언이라 목록에 link_target_missing과 link_target_ambiguous가 들어가고(결정 8, 같은 상수), 그 둘이 "4단계가 아직 쓰지 않는 종류"의 목록에서 빠진다. 견주는 쪽은 여전히 목록 전체를 정확히 같게 단언하고, 쓰지 않는 종류로 남은 cardinality는 그대로 가진 것이 아니라고 단언한다.
decision_hash: bac7ee57e748a7ec
---
# ADR-0122 새 수집 흐름 4단계의 링크 쪽

## 맥락

ADR-0115(#110)와 ADR-0119(#112)가 4단계의 객체 쪽을 정했고 링크 쪽은 "I-5b2"로 남겼다. 이 결정은 그 링크 쪽이다. 옛 흐름(`backend/internal/ingest/linkbuild.go`)은 링크 정의마다 To 쪽 행의 끝 키로 색인을 만들고, From 행마다 색인에서 대상을 찾아 대상이 0개면 `link_target_missing`, 2개 이상이면 `link_target_ambiguous`, 1개면 링크(`graph.NewLinkID`)를 만든다. 같은 id가 다시 나오면 뒤의 것이 이기고, 속성이 다르며 둘 다 걸러진 행이 아니면 앞 레코드에 `link_conflict`를 낸다. 걸러진 행(held)은 링크를 만들고 문제를 내지 않는다. 새 흐름은 이것을 쪽으로 나눠, 한 키를 많은 레코드가 공유해도 읽는 양이 제곱으로 늘지 않게, 쪽 크기·행 순서·레코드 번호와 무관하게, 끊고 이어도 같은 결과로 해야 한다. 차수(`:1` 끝)는 5단계(결정 7)이므로 이 단계는 후보 링크를 모두 `stage_link`에 둔다.

## 결정

1. **새 포트를 둔다.** `ingest.LinkRecomputer`(`BeginRecomputeLinks`, `RecomputeLinksPlace`, `RecomputeLinksPage`, `PutRecomputedLinks`, `RecomputeLinksReport`)와 Go의 `LinkMaker`(`MakeLinks`)이고, `RunRecomputeLinks`가 몬다. 객체 쪽 `Recomputer`는 건드리지 않아 잠긴 테스트가 그대로다. 자리는 실행 기록의 카운트 `recompute_links`에 두고(`calc_job`은 그대로 4) 객체 쪽이 `Finished`일 때만 시작한다. 한 쪽은 한 트랜잭션에 읽기와 쓰기가 아니라 "읽기(바꾸지 않음), Go가 만듦, 쓰기와 자리 이동이 한 트랜잭션"이다. 임대의 시도 번호는 읽기에도 확인하고, 같은 쪽을 두 번 쓰면 `CodeRunStage`로 거절한다. 이어받은 서버가 다른 링크 정의를 주면 `CodeConflict`다(정의 번호는 소스의 계획 안의 자리이므로 정의는 시작 때 값으로 고정한다).
2. **다섯 국면.** `ends`(이번 실행이 다시 읽기만 하는 레코드에 참조의 끝을 `calc_end`로 옮겨 적는다, 결정 3), `to`(To 끝의 `calc_ref`, SQL만), `from`(From 끝 풀기와 링크 만들기, Go), `conflicts`(같은 링크를 다른 속성으로 만든 레코드, SQL), `cells`(값을 주지 못한 칸, SQL). 끝을 읽는 국면의 자리는 (링크 정의, 끝 키 해시, 레코드) 순서의 키셋(`calc_end_key`: side, def_no, lookup_hash, rid 인덱스)이고, 링크를 읽는 국면의 자리는 링크 id다. 쪽의 끝은 인덱스로 먼저 정한다.
3. **다시 읽기만 하는 레코드의 끝.** 식별은 읽은 레코드의 끝만 `calc_end`에 쓴다. 확장이 더한 레코드(`why = Expanded`: 끝 키가 움직인 링크의 From 레코드와, 객체의 줄로 읽는 레코드)는 `calc_end`가 없다. `ends` 국면이 그 레코드마다 `record_ref`(역할 From, To)가 가진 끝을 `calc_end`로 옮겨(이미 있으면 그대로) 같은 문장으로 읽게 한다. 이런 레코드의 객체는 이번 실행의 묶음(`calc_bind`)이 아니라 식별 참조의 대상이다(`why = Expanded`일 때만. 바뀐 레코드가 묶이지 못했는데 옛 참조로 되돌아가면 안 되므로). 예외는 셋이다. (가) 그 객체는 등록부의 객체이고 이번 실행이 만든 새 객체가 아니므로 되돌려지지 않는다. 건너뛴 객체(`calc_left`)이거나 폐기 후보(`why = 4`)여도 끝의 대상이 되고 링크를 만든다(격리는 5단계). (나) 같은 레코드의 같은 (끝 쪽, 링크 정의) 끝이 이미 `calc_end`에 있으면 옮기지 않는다. 겹쳐 넣지 않고 이번 실행의 끝이 앞선다. (다) 이 국면의 쪽은 레코드 번호의 범위이고 쪽 쓰기와 자리 이동이 한 트랜잭션이라, 이어받은 서버는 다음 범위부터 하며 같은 범위를 다시 써도 (나)의 조건 때문에 겹치지 않는다.
4. **To 색인과 키 집계.** 끝 키(소스, 링크 정의, 해시)마다 To 쪽 대상 수(0, 1, 2는 "둘 이상")와 대상 하나를 센다. 센 곳은 영구 `record_ref`(역할 To, 이번 실행의 `calc_record`에 든 레코드는 뺌)와 이번 실행의 `calc_end`(side To)와 그 레코드의 객체다. 센 결과는 `calc_tokey`에 한 번만 쓰고 키를 처음 만나는 쪽이 센다. 대상이 같은 객체인지는 `min = max`로 알아서 한 키의 메모리가 키 수에 비례하지 않는다. 한 키를 To 레코드 m개와 From 레코드 m개가 공유하면 To 쪽 읽기는 m이고 From 쪽 읽기는 인덱스로 m이다(m = 300과 3m에서 문장마다 읽은 행의 비가 3.4배 이내이고, 크기마다 From 끝 하나에 16행 이하임을 테스트가 실행 계획으로 잰다. `enable_seqscan`은 건드리지 않는다). 레코드의 묶음은 `min`으로 한 값을 고른다(행을 찾은 순서에 매이지 않는다). 묶음 표가 몇 페이지뿐이면 계획기가 행마다 표를 처음부터 읽는다. 그 경우는 표가 작아 읽는 양의 절대값이 작지만 제곱이므로, 테스트의 m은 두 크기 모두 인덱스로 읽는 크기로 정했고 크기마다의 상한이 제곱인 계획을 잡는다(m = 150에서는 From 끝 하나에 수백 행을 읽어 상한에 걸린다). `record_ref_lookup`에는 소스 열이 없어 같은 정의 번호·해시의 다른 소스 행을 읽고 소스 번호로 거르는 한계가 있다(알려진 한계).
5. **From 끝 풀기.** 대상 0개면 `link_target_missing`(옛 흐름과 같은 이유 글), 2개 이상이면 `link_target_ambiguous`, 1개면 링크다. 걸러진 행은 문제를 내지 않는다. `calc_ref`의 From 참조는 대상이 링크 번호이고(풀리지 않았으면 대상 없음, 상태는 걸러진 레코드면 `Held`, 아니면 `Unresolved`), To 참조는 대상이 객체다. 객체가 없는 From 레코드(되돌려졌거나 식별이 풀지 못함)는 참조도 문제도 쓰지 않는다. 식별이 이미 냈기 때문이다. 끝 키 칸이 비었거나 형식이 맞지 않으면 링크도 문제도 없다(옛 `linkKey`).
6. **링크 값은 Go가 만든다.** 링크 id(`graph.NewLinkID`), 속성 문서(옛 `attrValues`와 같은 뜻), 등급(두 피드의 낮은 쪽), 출처 목록, 내용 해시(`graph.ContentHasher.Link`), 행 합(`graph.RowSum`)을 `stage_link`에 쓴다. 링크 번호는 이긴 레코드가 이미 가진 번호, 없으면 등록부(`link`)의 열린 링크의 번호, 없으면 `praxium_next_link_nos`의 새 번호(링크 id 순)다. 새 번호는 쪽 크기에 따라 달라지므로 테스트는 번호 없이 id로 견준다.
7. **같은 링크를 여러 레코드가 만들면 뒤가 이긴다**(결정 11). 전순서는 (소스 번호, 링크 정의, 걸러진 행은 뒤, 데이터셋 번호, 자연 키의 바이트)이고 가장 뒤가 이긴다. 객체의 값이 이기는 방향과 반대가 아니라 옛 흐름의 "나중 행이 이김"과 같다. 고름은 순수 함수(`ingest.ElectLinks`)가 쪽의 후보와 그 링크의 이긴 레코드(`calc_linkwin`)를 견줘 하므로 쪽 크기와 무관하다. 진 레코드(밀려난 이전 승자 포함)는 `calc_linklost`에 남고 From 참조가 미해결(`Unresolved`, 대상 없음)이 된다. 다음 실행의 확장이 그 레코드를 다시 출발 레코드로 삼는다.
8. **문제의 종류는 새로 만들지 않는다**(ADR-0119 결정 2, REQ-680). `link_target_missing`, `link_target_ambiguous`는 끝 쪽에서, 같은 링크를 다른 속성으로 만든 레코드의 알림은 `source_conflict`(값이 다른 쪽에 진 일)로 내고 이유 글이 "the link "로 시작해 객체의 충돌과 구별한다. 옛 흐름의 `link_conflict`는 이름만 다르고 레코드와 뜻이 같다(알림은 (승자와 진 레코드) 사슬에서 속성이 다른 다음 레코드가 있는 앞 레코드, 둘 다 걸러지지 않았을 때). 링크 속성의 칸이 값을 주지 못한 경우는 이긴 레코드에 `invalid_value`(칸 이름 없음)로 낸다. `StepFourProblemKinds()`는 `source_conflict`, `link_target_missing`, `link_target_ambiguous`를 돌려주며 같은 상수를 쓴다(`invalid_value`는 받는 쪽이 만드는 종류라 가지지 않는다). `cardinality`는 5단계다.
9. **생성기를 12로 올린다.** 새 표 `calc_tokey`, `calc_linkwin`, `calc_linklost`(계산 표라 `ComputeTables()`에 들어 한 번의 실행이 끝나면 함께 비워진다)와 인덱스 `calc_end_key`, `calc_linklost_ref`를 더한다. 단계는 `to_regclass`로 지키고 두 번 해도 같다. 읽는 쪽 표나 이미 있는 표는 바꾸지 않는다(REQ-680).
10. **객체 상한 기본값이 쪽 한도(`Lines`)를 따르게 하는 변경은 하지 않는다.** 검토에서 `ObjectMax`가 0이면 `Lines`와 같게 하자는 제안이 있었다. 지금의 약속은 "기본 상한은 쪽 한도와 따로"이고, 잠긴 `TestRcpWhatTheRunReadsIsTheReferencesThatStandAndOnlyOfTheObjectsThatAreNotCandidatesForRetirement`(쪽 크기 1, `ObjectMax` 0, 줄 2개인 객체를 포함한 객체 4개를 단언)가 그 약속을 지킨다. 바꾸면 그 단언이 깨지고, 쪽 크기를 줄인 실행이 줄이 많은 객체를 건너뛰게 되어 쪽 크기와 무관하다는 약속(REQ-873과 같은 뜻)과도 부딪힌다. 그래서 하지 않는다.

## 고친 것 (ADR-0115의 바뀌는 문장)

| ADR-0115 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| 범위 (링크 쪽) | 링크 쪽은 I-5b2다 | 이 결정이 링크 쪽이다 (결정 1부터 8) |
| 9항 (문제) | 링크의 종류의 이름은 링크를 만드는 단계가 더한다 | `link_target_missing`, `link_target_ambiguous`를 더하고 속성 충돌은 `source_conflict`로 낸다. 새 종류는 만들지 않는다 (결정 8) |
| 10항 (생성기) | 10으로 올린다 | 12로 올린다 (결정 9, 세대 11은 먼저 병합된 변경의 것) |

## 잠긴 테스트의 변경

단언을 줄이거나 임계값을 넓힌 것은 없다. 변경한 잠긴 테스트는 `amends_tests`에 있다. 하나는 숫자(생성기 버전)이고, 하나는 "4단계가 가진 종류"의 목록이 정확히 같다는 단언의 기대 값에 링크의 두 종류가 들어간 것이다(그 둘은 이제 4단계가 쓰므로 가지지 않는다는 단언에서 빠진다. `cardinality`는 여전히 가지지 않는다고 단언한다).

## 옛 흐름과 다른 점

- 이유 글의 행 표시는 옛 흐름의 줄 번호가 아니라 `row <키>`다(새 흐름에는 줄 번호가 없다). 객체의 이름은 `stage_object`나 등록부에서 읽는다.
- 같은 링크를 만든 레코드가 쪽을 가로질러 있어도 사슬의 비교는 같은 전순서에서 이웃끼리다. 옛 흐름이 처리 순서로 이웃을 본 것과 같은 결과가 되도록 순서를 옛 흐름의 처리 순서(소스, 링크 정의, 행의 뒤에 걸러진 행)에 맞췄다.
- 소스가 둘 이상일 때는 소스 번호 순이 잠정이다(결정 11).
- **걸러진 행의 끝 문제.** 옛 흐름의 코드(`linkbuild.go`의 `link`)는 걸러져 기다리는 행에도 `link_target_missing`, `link_target_ambiguous`를 낸다. ADR-0052 결정 3("기다리는 동안 문제는 보고하지 않는다")과 REQ-435("걸러져 기다리는 행의 링크는 값은 읽되 문제는 남기지 않는다")와 맞지 않는 옛 코드의 구멍이므로 새 흐름은 내지 않는다. 대조 테스트는 옛 흐름이 걸러진 행에 낸 문제를 따로 세고(0보다 커야 한다) 나머지를 정확히 견준다.
- **소스를 가로지르는 식별.** 옛 흐름은 식별의 상태가 소스마다 따로라, 객체를 만들지 않는 피드가 다른 소스가 만든 객체를 찾지 못하고 `orphan_value`가 된다. 새 흐름은 KG의 키 색인으로 찾는다(REQ-478의 방향). 그래서 대조의 둘째 소스는 자기 객체를 만드는 매핑이고, 두 소스의 레코드가 같은 링크를 만드는 경우(뒤 소스가 이기고 앞 소스의 레코드는 미해결과 알림)는 따로 테스트가 정한다.
- 차수(`:1` 끝)로 옛 흐름이 버린 링크도 이 단계는 후보로 둔다(결정 7의 5단계). 대조의 사례에는 차수에 걸리는 링크가 없다.

## 이유

- 끝의 읽기를 키로 모으고 센 결과를 한 번만 두는 것이, 한 키를 공유하는 레코드가 많은 실제 데이터에서 제곱으로 늘지 않는 길이다.
- 이어 가기 값을 키셋 커서로 하면 쪽 크기와 레코드 번호와 무관하게 같은 결과가 된다. 승자를 이긴 레코드를 담은 표와 견줘 고르는 것은 쪽이 달라도 같은 전순서의 최댓값을 주기 때문이다.
- 새 문제 종류를 만들지 않으면 CHECK와 거울 목록, 개수 테스트를 건드리지 않고 REQ-680과 부딪히지 않는다(ADR-0119와 같은 방식).

## 대안과 기각 사유

- **링크 속성의 충돌에 새 종류 `link_conflict`를 만든다.** 기존 표 두 개의 CHECK를 `ALTER`해야 하고 REQ-680과 부딪힌다. 기각. 이유 글의 머리말로 구별한다.
- **From 끝을 읽을 때마다 To 레코드를 다시 읽는다.** 한 키를 m개가 공유하면 m의 제곱을 읽는다. 기각.
- **다시 읽기만 하는 레코드의 끝을 읽을 때마다 `record_ref`와 합친다.** 문장마다 합집합이 들어가 복잡해지고 인덱스를 못 쓴다. 한 국면에서 옮겨 두는 것이 단순하다.

## 알려진 한계와 넘김

- 소스가 둘 이상일 때 `record_ref_lookup`에 소스 열이 없어 다른 소스의 같은 정의 번호·해시 행을 읽고 거른다. 소스 열을 인덱스에 더하면 풀리지만 읽는 쪽 표의 변경이라 이 결정에서 하지 않았다.
- 객체 상한 기본값이 `Lines`를 따르게 하는 변경은 결정 10의 이유로 하지 않았다. `Lines`가 기본이 아닐 때 한도가 쪽보다 클 수 있다는 ADR-0119의 한계는 그대로다.
- 차수(`:1` 끝)와 `cardinality`는 5단계다. 이 단계의 `stage_link`는 차수로 빼기 전의 후보다.
- `calc_linkwin`에서 지는 쪽의 알림은 승자와 사슬로 이어질 때만 난다. 승자가 같은 속성이면 알림이 없다(옛 흐름과 같다).

## 결과

- 4단계가 객체와 링크를 모두 `stage_*`에 둔다. 옛 `Runner`와 같은 입력으로 링크 id, 속성, 문제, 중복의 승자가 같음을 테스트가 견준다(7단계, 소스 둘, 걸러진 행 포함).
- 쪽 크기, 행 순서, 레코드 번호, 이어받은 서버에 상관없이 같은 `stage_link`이고, 보고의 수는 실제로 쓴 줄 수다.
- 진 링크의 From 참조는 미해결이고, 없는 객체를 가리키는 링크는 없다.
