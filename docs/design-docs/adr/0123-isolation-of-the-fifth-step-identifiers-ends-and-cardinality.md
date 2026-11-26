---
title: ADR-0123 새 수집 흐름 5단계의 앞부분 — 격리 ① 식별자 ② 링크의 끝 ③ 차수
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/iso_make_test.go
  - backend/internal/pgingest/iso_edge_test.go
  - backend/internal/pgingest/iso_flow_test.go
  - backend/internal/pgingest/iso_parity_test.go
  - backend/internal/pgingest/iso_scale_test.go
  - backend/internal/pgingest/iso_damage_test.go
  - backend/internal/pgschema/iso_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0115-recompute-objects-step-of-the-new-ingestion-flow.md
  - docs/design-docs/adr/0086-redesign-ingestion-for-tens-of-millions-of-objects.md
updates_docs:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
amends:
  - adr: "0115"
    what: 범위의 차수(5단계 격리로), 생성기 버전(10항)
  - adr: "0086"
    what: 불변식 I3(준비 테이블은 넣기만) — 4단계의 되돌림(ADR-0119)과 5단계의 격리는 단위의 쓰기와 위치 이동이 한 트랜잭션인 안에서 계산 표의 줄을 빼고 calc_ref를 바꾼다
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
test_change_reason: 단언을 줄이거나 임계값을 넓힌 것은 없다. TestControlReservesThePartitionPrefix는 생성기 버전의 숫자를 단언하므로 12에서 13으로 바꾼다(이 결정 8). 파티션 접두어를 예약한다는 단언은 그대로다.
decision_hash: e04c430a06913b12
---
# ADR-0123 새 수집 흐름 5단계의 앞부분 — 격리

## 맥락

4단계(ADR-0115, ADR-0119, ADR-0122)가 객체와 링크를 `stage_object`, `stage_link`, `stage_ident`에 둔다. 5단계는 이것을 KG와 견줘 `ingest_diff`를 만드는데, 그 전에 KG가 가질 수 없는 줄을 빼야 한다. [식별과 영향 집합](../ingestion-at-scale-identity.md) 4절의 순서는 ① 식별자의 충돌 ② 끝이 없는 링크 ③ 차수이고 한 번씩이며, 격리한 줄은 레코드 참조의 미해결이 되어 다음 실행이 다시 본다. 폐기되는 객체의 링크는 격리가 아니라 함께 폐기다. 옛 흐름은 차수를 `linkbuild.go`의 `checkCardinality`(배치 안에서 링크 id 순으로 `:1` 끝의 뒤 링크를 빼고 `cardinality`)가 했고, 그래프의 상태 검사(`graph/plan_state.go`)는 결합마다 링크 수를 센다. 이 결정은 5단계의 앞부분(격리)이고 차이(`ingest_diff`)는 다음 결정이다.

3단계가 이미 보장하는 것: 식별의 주장 라운드(`pgingest/identify_end.go`의 `claimsSQL`, 80~108행)가 이번 실행의 주장(`calc_claim`, 걸러진 행 제외)마다 승자를 정한다. 승자는 KG에서 그 식별자를 가졌고 이번에도 주장하거나 다시 읽지 않는 레코드의 해결된 참조가 남은 객체(`hold`, 95~99행), 없으면 id가 작은 객체(`win`, 100~103행)이고, 진 주장은 `LostClaim`과 `identity_conflict`다(104~107행). 4단계는 해결된 주장만 읽어(`recompute_sql.go`의 `rcpClaimsSQL`, 69~73행) 체계마다 값 하나를 `stage_ident`에 둔다(`recompute_values.go`의 `identsOf`, 355행). 그래서 ①은 그 라운드를 다시 하지 않고, 그 라운드가 볼 수 없던 것만 잡는다. 다시 읽기만 하는 레코드(`why = Expanded`)의 유지된 주장은 `calc_claim`에 없고, KG의 주인이 주장 참조 없이 식별자를 가진 경우(행 단위 경로나 개발 적재가 넣은 것), 범위(`scope`)는 라운드가 보지 않는다.

## 결정

1. **새 포트를 둔다.** `ingest.Isolator`(`BeginIsolate`, `IsolatePlace`, `IsolatePage`, `PutIsolated`, `IsolateReport`)와 Go의 `IsolateMaker`(`MakeIsolated`)이고 `RunIsolate`가 몬다. 구현은 `ingest.IsolationMaker`(모델의 결합)와 `pgingest`다. `calc_job`은 5(`CalcJobIsolate`)이고 이어 갈 위치는 실행 기록의 카운트 `isolate`에 둔다. 4단계의 객체와 링크가 모두 끝난 실행에서만 시작한다. 국면은 `idents`(①), `ends`(②), `seats`(③)이고 한 번씩이며, 한 페이지는 "읽기(바꾸지 않음), Go의 결정, 쓰기와 위치 이동이 한 트랜잭션"이다. 같은 페이지를 두 번 쓰면 `CodeRunStage`, 임대를 잃은 서버는 읽기도 `CodeRunLeaseLost`다.
2. **① 식별자.** `stage_ident`를 (체계, 값, 범위)의 바이트 순서로 페이지마다 읽는다(한 식별자의 줄은 한 페이지에 모두 든다). 한 식별자를 가지는 객체는 이번 `stage_ident`의 객체와, KG에서 그 식별자를 가졌고(`object_identifier`의 열린 줄) 유지하는 객체다. 유지한다는 것은 이번 실행이 그 객체를 계산하지 않고(`stage_object`에 없음) 살아 있고 이번에 퇴역하지 않는 것(건너뛴 객체 `calc_left` 포함), 또는 이번 `stage_ident`에도 그 식별자가 있는 것이다. 둘 이상이면 승자는 2.4의 규칙(`ingest.ElectIdent`)으로 KG에서 가졌고 유지하는 객체, 없으면 id가 가장 작은 객체다. 진 객체는 그 식별자 줄만 빠지고 객체는 남는다. 그 주장의 참조는 진 주장이다: 이번 실행의 참조(`calc_ref`)는 상태를 바꾸고, 다시 읽기만 하는 레코드의 유지된 참조(`record_ref`)는 같은 키의 진 주장 줄을 `calc_ref`에 더한다. 레코드마다 `identity_conflict`(3단계와 같은 이유 글)다. 참조가 이름 대는 값의 해시는 Go가 만든다(수신이 쓰는 `kernel.ContentHash`, P3).
3. **② 링크의 끝.** `stage_link`를 번호 순서로 페이지마다 본다. 끝이 이번 `stage_object`에도 없고 KG에 살아 있지도 않은 객체(되돌린 새 객체, 이미 퇴역한 객체)이면 링크를 빼고(`IsolatedEndMissing`) 이긴 레코드에 `link_target_missing`, From 참조는 대상 없는 미해결이다. 끝이 이번에 퇴역하는 객체(`calc_object.why = 4`이고 `stage_object`에 없음)이면 **격리하지 않고** 함께 폐기될 것으로 적는다(`IsolatedEndRetires`, 문제 없음, From 참조만 미해결). 실제 폐기는 다음 결정의 차이가 낸다. 끝이 없는 것을 먼저 본다. 건너뛴 있는 객체와 이번 실행의 새 객체는 정상 끝이다. 양 끝이 같은 객체여도 한 줄이다.
4. **③ 차수.** `:1` 끝은 (방향, 끝 객체, 결합)이다: 결합의 차수가 `:1`로 끝나면 시작, `1:`로 시작하면 끝(그래프 검사와 같은 뜻이고, 결합은 `model.Registry.BindingFor`가 끝 객체의 타입으로 고른다). 결합이 없는 타입 쌍은 `:1` 끝이 없다. **설계 책임자의 결정대로 KG에 이미 있고 이번에도 남는 링크가 `:1` 끝을 지키고, 없으면 링크 id가 작은 것이 받는다.** 겨루는 집합은 이번 `stage_link`와, 이번 실행이 바꾸지도 폐기하지도 않는 KG의 링크다. 후자는 `stage_link`에 없고, 격리되지 않았고, 끝이 이번에 퇴역하지 않고, 이번 실행이 읽지 않는 레코드의 해결된 From 참조가 있거나 해결된 From 참조가 아예 없는(수집이 아닌 길로 들어온) 링크다. 이번 실행이 만든 레코드가 모두 다시 읽혔는데 다시 만들지 않은 KG 링크는 차이가 폐기하므로 겨루지 않는다. 같은 링크가 KG에도 있고 `stage_link`에도 있으면 한 링크다. `stage_link`를 id 순서로 한 번 지나며(페이지마다 Go의 `seats`) `:1` 끝이 모두 비었으면 차지하고 하나라도 차 있으면 진다. 차 있다는 것은 앞에서 차지한 링크(앞 페이지는 `calc_seat`, 이 페이지는 메모리), 손대지 않는 KG 링크, 그리고 KG에 없는 링크에게는 KG에 있고 다시 staged된 뒤의 링크(그 차례에 차지한다)다. 진 링크는 아무것도 차지하지 않으므로 `1:1`의 사슬도 한 번에 정해진다. 진 링크는 빠지고 이긴 레코드에 `cardinality`(옛 `cardinalityReason`과 같은 글), From 참조는 진 주장이다.
5. **격리한 줄은 `stage_*`에서 뺀다(표시만 하지 않는다).** 뺀 것은 `calc_isolated`(종류, 객체나 링크의 번호, 식별자, 링크 id, 국면, 이유 번호, 이유 글. 키는 뺀 것이라 한 줄은 한 번)에 남고, 참조의 상태는 `calc_ref`에서 바뀐다. 걸러진 행의 레코드는 문제를 내지 않고 참조는 `Held`다.
6. **문제 종류는 새로 만들지 않는다**(REQ-680). `StepFiveProblemKinds()`는 `identity_conflict`, `link_target_missing`, `cardinality`이고 문장은 같은 상수로 쓴다. `cardinality`는 4단계가 가지지 않는다(ADR-0122와 같다).
7. **읽기는 한 행씩 색인으로 한다.** 객체, 식별자, KG 링크, 참조를 찾는 것은 스칼라 부분 질의나 `OFFSET 0`을 붙인 측면 부분 질의다. `EXISTS`나 조인은 계획기가 표 전체를 읽는 조인으로 바꿀 수 있고(작은 표가 그렇게 이끈다) 한 식별자의 범위는 크기를 알기 어렵다(한 체계가 표의 대부분일 때가 많다). 식별자의 페이지는 바이트 순서의 색인(`COLLATE "C"`)을 끝 식별자의 줄 수까지 `LIMIT`으로 읽는다.
8. **생성기를 13으로 올린다.** 표 `calc_isolated`, `calc_seat`(키는 `:1` 끝이라 한 `:1` 끝에 링크 하나)와 인덱스 `stage_ident_value`, `stage_link_ref`를 더한다. 계산 표라 `ComputeTables()`에 들어 실행이 끝나면 함께 비워지고 KG 삭제(ADR-0125)도 따라온다. 단계는 `to_regclass`로 지키고 두 번 해도 같다. 이미 있는 표는 바꾸지 않는다.

## 고친 것 (ADR-0115의 바뀌는 문장)

| ADR-0115 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| 범위 (링크의 차수) | 링크 쪽(`stage_link`, 끝 풀기, 차수)은 I-5b2의 몫이다 | 차수는 5단계 격리 ③이다 (결정 4) |
| 10항 (생성기) | 8로 올린다 | 13으로 올린다 (결정 8) |

## 잠긴 테스트의 변경

단언을 줄이거나 임계값을 넓힌 것은 없다. `TestControlReservesThePartitionPrefix`의 생성기 버전 숫자만 12에서 13으로 바뀐다.

## 옛 흐름과 다른 점

- **KG에 있는 링크가 `:1` 끝을 지킨다.** 옛 흐름은 배치 안의 링크 id 순으로 앞의 링크를 남겼다. 새 링크의 id가 KG의 링크보다 작으면 옛 흐름은 새 링크를 남기고 새 흐름은 KG의 링크를 남긴다(`TestIsoWhereTheOldPipelineKeptTheLinkOfTheSmallerIDTheNewFlowKeepsTheLinkTheKGHas`). 새 링크끼리는 같다(`TestIsoTheLinksTheCardinalityTakesOutAreTheOnesTheOldPipelineSetsAsideFromTheSameCaptures`).
- **`:1` 끝은 결합마다다.** 옛 흐름은 링크 타입과 끝 객체로 셌고 새 흐름은 그래프 검사처럼 결합마다 센다. 한 링크 타입의 다른 결합에 있는 링크는 겨루지 않는다.
- **걸러진 행은 차수 문제를 내지 않는다.** 옛 흐름의 `checkCardinality`는 걸러진 행에도 냈다(ADR-0052 결정 3, REQ-435와 맞지 않는 옛 코드의 구멍, ADR-0122와 같다).
- **끝이 없는 링크.** 옛 흐름에는 같은 것이 없다(배치에 없는 객체를 가리키는 링크는 그래프 검사가 배치 전체를 거절했다). 그래서 대조는 차수와 4단계의 끝 문제만 한다.

## 이유

- 빼는 것이 표시보다 안전하다. 표시만 하면 차이, 정리, 보고가 모두 `calc_isolated`를 반조인해야 하고, 하나라도 빠뜨리면 격리한 줄이 조용히 적용된다. 빼면 뒤 단계가 `stage_*`와 `calc_ref`를 그대로 진실로 읽는다. 4단계가 되돌린 객체를 같은 방식으로 뺐다(ADR-0119 결정 1).
- 차수를 id 순서로 한 번 지나는 것이 결과를 순서 하나에만 매이게 한다. `:1` 끝마다 따로 승자를 정하면 `1:1` 사슬에서 다른 끝에서 진 링크가 끝을 비우지 못해 필요 없이 빠지는 링크가 생기고, 다음 실행도 같은 결과라 영영 들어오지 못한다.
- KG의 링크가 `:1` 끝을 지키면 링크가 실행마다 흔들리지 않는다(식별자 2.4와 같은 원칙).

## 대안과 기각 사유

- **`calc_isolated`에 표시만 하고 `stage_*`는 그대로 둔다.** I3(준비 테이블은 넣기만)에 맞지만 읽는 단계가 모두 반조인해야 하고 빠뜨리면 실패가 열린다. 기각.
- **두 번 지나기(KG에 있는 링크 먼저, 그다음 나머지).** 첫 통과의 페이지가 KG에 없는 링크를 걸러 읽으므로 한 단위가 실행의 링크 전부를 읽을 수 있다. 한 번 지나며 KG에 있는 뒤의 링크를 먼저 차지한 것으로 세는 것과 결과가 같다(KG가 이미 규칙을 어긴 경우는 아래 한계). 기각.
- **3단계의 주장 라운드를 5단계에서 다시 한다.** 뜻의 구현이 둘이 된다. 기각.

## 알려진 한계와 넘김

- I3과의 관계: 4단계(ADR-0119, ADR-0122)와 같이 이 단계도 계산 표의 줄을 삭제하고 `calc_ref`를 바꾼다. 단위의 쓰기와 위치 이동이 한 트랜잭션이라 이어 가기는 그대로다.
- KG가 이미 한 `:1` 끝에 링크 둘을 가진 경우(모델의 차수가 바뀐 뒤): 손대지 않는 KG 링크는 모두 차지하고, 다시 staged된 KG 링크끼리는 id가 작은 것이 이긴다. 다시 staged된 KG 링크가 그런 이유로 지면, 그 링크 때문에 먼저 진 새 링크는 다음 실행에서 다시 본다.
- 결합을 모르는 SQL은 링크 타입의 어느 결합이라도 `:1`인 끝에서 KG 링크를 읽는다(확장의 카드와 같은 넓힌 어림). 허브 객체의 한 링크 타입에 `N:M` 결합과 `1:N` 결합이 함께 있으면 그 허브의 링크를 페이지마다 읽는다.
- 바뀌지도 폐기되지도 않는 KG 링크의 정의(결정 4)는 다음 결정의 차이가 같은 정의로 링크를 폐기해야 맞는다. 차이를 만들 때 이 정의를 테스트로 묶는다.
- 작은 표에서는 계획기가 부분 질의마다 표를 처음부터 읽어 제곱이 된다(ADR-0122와 같은 관찰). 테스트의 m은 두 크기 모두 색인으로 읽는 크기(식별자 600, 링크 1,000)로 정했다.

## 결과

- 5단계의 차이가 읽는 `stage_link`의 끝은 모두 이번 `stage_object`이거나 KG에 살아 있고 퇴역하지 않는 객체이고, 한 `:1` 끝에 남는 링크와 한 (체계, 값, 범위)를 가지는 객체는 하나다(SQL 불변식 테스트).
- 격리된 레코드는 미해결이나 진 주장이라 다음 실행의 출발 레코드가 된다(ADR-0114의 (나)).
- 페이지 크기, 객체와 링크의 번호를 받은 순서, 이어받은 서버에 상관없이 같은 결과이고, 한 키를 m개가 공유해도 한 단위가 읽는 행은 그 단위의 줄에 비례한다.
