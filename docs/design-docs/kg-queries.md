---
title: KG에 묻기 — 사전 정의 질의, 영향 범위, 프로젝션과 엔진
kind: guide
status: active
follows:
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
---
# KG에 묻기

KG에 묻는 길 전체의 설계예요. 사전 정의 질의 13종, 모든 답에 붙는 엔벨로프, 영향 범위의 뜻, 그것을 만드는 프로젝션과 질의 엔진, 바깥으로 내는 면 셋을 정해요. 코드는 아직 없어요(백엔드는 Go로 정해졌어요. ADR-0006). 타입 이름은 영어이고, 표준 모델의 최종 이름은 ADR-0007의 표준 모델을 따라요. 이 문서의 이름은 예시예요. **질의는 KG 하나 안에서 돌아요.** KG에 든 것을 묻는 길은 모두 `/v1/kgs/{kg}/...` 아래에 있고, KG 사이를 가로지르는 질의는 없어요.

## 1. 목적과 다루는 질문

- 누가 KG에 어떤 물음을 어떤 모양으로 할 수 있나. "이것이 죽으면 무엇이 어떻게 되나"를 이중화, 이어짐, 모르는 것까지 어떻게 정확히 따지나.
- 묻는 쪽이 많아도 저장소가 멈춰도 답이 이어지게 하려면 무엇을 나누나. 제품 책임자의 숫자(1초, 수 초)는 아직 근거가 없는 목표예요. 무엇을 측정해서 확인하나(12절).

## 2. 개요

```
 사람·시스템 ─┐                       ┌ SQL: find get neighbors(얕게) containment changes
 에이전트(MCP) ┼ API ─ 닫힌 질의 열셋 ─┤      versions quality health scopes competency
 다른 시스템 ──┘   + 봉투 + 읽기 권한 거르기 └ 엔진: impact exposure paths
 변경의 이어짐 ─▶ 질의 엔진(여러 벌): 메모리의 투영을 늘어난 만큼만 고친다
 (부르는 쪽은 엔진을 직접 부르지 않는다. 저장소가 멈춰도 올려 둔 투영으로 답한다)
```

## 3. 사전 정의 질의 13종

HTTP 길은 [REST API 설계](rest-api.md)가 정하고(`/v1/kgs/{kg}/queries:<질의 이름>`. `{kg}`는 KG의 ID이고 운영의 KG는 `main`으로도 가리켜요), 여기서는 질의의 이름과 칸으로 적어요. 칸 이름은 계약 단위(`core.contract`)가 그대로 쓸 이름이에요.

### 공통 입력

| 칸 | 뜻 |
|---|---|
| `kg_version_id` | 물을 KG 버전의 ID("그 버전 때"). 없으면 "지금"(변경의 맨 끝). 어느 자리까지 반영했는지가 엔벨로프에 적힌다 |
| `view` | 보는 기준. `as_designed`(`configured`와 `declared`만) 또는 `as_observed`(관측이 있으면 관측, 없으면 설계. 기본) |
| `min_seq`, `wait_timeout` | 적어도 이 변경 번호까지 읽을 수 있게 된 답을 요구해요. 큰 적용은 배치가 끝날 때만 읽기 워터마크(`readable_through`)가 올라서 반쯤 든 배치를 보지 않고, `serving_pin`도 이 자리를 봐요. 답에 변경 번호가 실려요. 복제본이 못 미치면 주 저장소로 가거나 `wait_timeout`까지 기다리거나 거절해요(ADR-0009, postgres-operations.md). 세대(`as_of.generation`)가 달라졌으면 `conflict.generation_changed`로 답해요. 기대와 대조(`confirmed`, `not_yet`, `contradicted`, `unverifiable`)는 ingestion-extensions.md 3절 |
| `max_staleness` | **소스에서 본 값이 얼마나 낡아도 되나** 하나만 뜻해요. 이보다 오래된 속성 값과 관측은 쓰지 말라는 한도예요. 영향 범위는 넘은 관측을 **알 수 없음으로 읽고** 설계 기준(`as_designed`)으로 물러나며, 그렇게 했다고 답에 표시해요(5.7절). 복제가 따라오기를 기다리는 것은 이 칸이 아니라 `min_seq`와 `wait_timeout`이에요 |
| `limit`, `cursor` | 페이지네이션. `limit`의 기본 100, 상한 1천은 기본값(설정)이다. `cursor`는 객체 ID(엔진의 내부 번호가 아니에요), 정렬 키, `change_seq`, KG의 세대를 품어서, 쪽 도중에 변경이 들어와도 같은 자리로 이어지고, 엔진 벌이 바뀌어도 이어져요 |
| `shape` | `summary`(수와 묶음 먼저, 기본) 또는 `items`. MCP에서는 `detail`로 `brief`와 `full`을 고른다 |

### 질의마다

| 질의 | 입력 필드 | 출력 필드 | 한도와 페이지네이션 |
|---|---|---|---|
| `find` | `object_type`(`include_subtypes`), `trait`, `attributes`(이름, 연산, 값의 목록. 연산은 `equals`, `in`, `prefix`, `range`), `identifier`(종류, 값), `text`, `scope_id` | `counts_by_type`, `items`(`object_id`, `object_type`, `display_name`, `matched_on`) | 인덱스로 답한다. `limit`/`cursor`. 읽을 수 없는 타입의 객체는 나오지 않는다. 가려진 속성을 찾는 조건으로 쓰면 거절한다 |
| `get` | `object_id`나 `identifier`, `include`(`attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`) | 객체, 속성(`name`, `value`, `unit`, `grade`, `basis`: 근거의 종류, `seen_at`, `stale`)과 가장 나쁜 신선도, 식별자들, 유효한 버전 구간, `provenance_refs`(원본 레코드의 참조와 규칙의 버전) | 한 객체. 링크는 수만 준다(`neighbors`로 읽는다) |
| `neighbors` | `object_id`, `link_types`, `meanings`, `direction`(`out`, `in`, `both`), `max_steps`, `peer_types` | `groups`(`link_type`, `meaning`, `direction`, `peer_type`, `count`)를 먼저, 그다음 `items`(`link_id`, `link_type`, `peer_object_id`, `peer_type`, `grade`) | `max_steps` 기본 1, 상한 3은 가정. 허브는 `groups`만 주고 `items`는 `limit`로 자른다 |
| `containment` | `object_id`, `direction`(`up`, `down`), `depth`, `child_types` | `ancestors`(건물, 방, 랙의 차례), `children_summary`(타입별 수), `subtree_size`, `items` | 깊이 기본 1. 자식은 페이지네이션 |
| `paths` | `from_object_id`, `to_object_id`, `via_meanings`(기본 `connected_to`), `link_types`, `max_steps`, `top_k`, `dead`, `dead_links` | `reachable`(`yes`, `no`, `unknown`), `paths`(`steps`, `length`, `weakest_grade`), `found_count` | `top_k` 기본 3, 상한 10, `max_steps` 상한은 가정. 시간 예산을 넘기면 `truncated` |
| `impact` | `dead`, `dead_links`, `site_failure`, `dead_scope_id`, `mode`(`both`, `facts_only`, `with_assumptions`), `shape` | 5.7절 | 시드 수 상한 1,000은 가정. 결과는 요약을 먼저 주고 `items`는 페이지네이션 |
| `exposure` | `dead`(없어도 된다), `scope_id`, `target_object_id`, `cut_order_max` | `tight_groups`, `cut_sets` | 차수 기본 2(기본값(설정)), 상한 3은 가정. `target_object_id`가 있을 때만 `cut_sets`를 센다 |
| `changes` | `from_version`/`to_version`(버전 ID) 또는 `from_change_seq`/`to_change_seq`, `object_types`, `source_id`, `scope_id`, `change_kinds`(`added`, `changed`, `retired`) | 타입별·종류별 수, 그다음 `items`(`object_id`나 `link_id`, `change_kind`, `changed_fields`, `change_seq`) | 변경의 기록에서 바로 읽는다. 페이지네이션 |
| `versions` | `since_version`(버전 ID), `limit` | 확정 버전들(`kg_version_id`, `change_seq`, `created_at`, `definition_version`, `input_digest`, `change_counts`, `is_head`, `projection_state`: `ready`, `building`, `none`) | 페이지네이션 |
| `quality` | `scope_id`, `source_id`, `metric`, `kg_version_id` | 지표와 목표. 자세한 칸은 ADR-0013이 정한다([데이터 관측성](data-observability.md)) | 지표 수가 적다 |
| `health` | `resource`(소스, 데이터셋, 피드, 타입, 속성, 링크 타입, 파이프라인), `scope_id` | 자원의 헬스 상태와 이유. 자세한 칸은 ADR-0013이 정한다 | 낮은 비용 |
| `scopes` | `scope_id`(없으면 목록) | 저장해 둔 범위(타입, 포함 뿌리, 필터링), 만든 그룹, 이 버전에서의 `count` | 페이지네이션. 읽기만 한다. 만드는 길은 10절 |
| `competency` | `question_ids`, `object_ids`, `filter`, `group_by`, `mode`, `breakdown`, `history`, `include` | 충족률, 못 채운 객체, 빈 곳. 자세한 칸은 ADR-0015([역량 질문](competency-questions.md)) | 객체마다의 표시를 세어 답한다. `scope_id` 대신 `filter` |

`impact`와 `exposure`와 `paths`는 비용이 큰 질의예요. 질의마다 **비용 등급**(낮음: `get`, `find`, `versions`, `quality`, `health`, `scopes` / 중간: `neighbors`, `containment`, `changes`, `paths`, `competency` / 높음: `impact`, `exposure`)과 시간 예산을 선언하고, 넘기면 잘라서 `truncated`로 알려요. 숫자는 측정해서 정해요. 수를 내는 모든 칸(`counts_*`, `total`, `groups_touched`, `link_counts`, `subtree_size`)은 **가려진 것을 뺀 수**예요(11절).

### 첫 셋의 기준 구현이 정한 읽기 (ADR-0026)

`find`, `get`, `neighbors`는 `core.query`에 기준 구현이 있고, 위 표가 비워 둔 곳은 이렇게 읽는다.

- **읽기 워터마크**: 커서 > `kg_version_id` > `change_seq`(덧붙임) > 최신 순이고, 버전과 변경 번호를 함께 주면 거절한다. 커서는 처음 읽은 변경 번호로 이어서 읽는다.
- **`shape`**: `summary`는 수와 묶음만, `items`는 항목도 준다. **보는 기준**: `as_designed`는 `configured`와 `declared` 값만 본다. `max_staleness`보다 묵은 속성은 `stale`이고 값이 없다.
- **`get`의 `include`**: 안 주면 `attributes`. 속성은 항상 `grade`, `basis`, `seen_at`, `stale`을 단다. `evidence`는 등급 수와 가장 묵은 속성(`worst_freshness`)을 더한다.
- **`neighbors`**: 순회는 전체 그래프에서 하고 가려진 객체의 너머로도 가며, `max_steps`번째 걸음에서 만난 가려진 객체는 `hidden_reached`, 앞 걸음에서 지난 것은 `hidden_passed`다. 항목은 (걸음, 링크 ID) 순서다.
- **거절**: 가려진 속성, `volatile` 속성, `scope_id`를 조건으로 쓰면 거절한다(아직 서비스하지 않는다).
- **길과 기본 `shape`**(ADR-0027): 셋은 `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`로 서고, 목록의 짧은 길(`GET /objects`, `/objects/{object_id}/links`)은 같은 질의를 부른다. 질의는 기본이 `summary`이고 **목록 길은 기본이 `items`**다(목록을 부르는 사람은 항목을 보려고 부른다). POST의 답에는 ETag를 달지 않는다. 읽기 권한 필터링은 호출자의 역할에서 온다: 읽기 역할은 모든 타입, 민감 등급 `open`까지, 전체 범위의 부여 하나를 가진 것으로 읽고, 저장된 부여가 서면 그것으로 바뀐다.
- **측정한 것**은 [KG 읽기의 측정](kg-read-measurements.md)에 있다. `get`과 평범한 `neighbors`는 크기를 타지 않고(0.1ms 안팎), `find`는 객체 수에 선형이다(10만 객체에서 0.3~0.5초).

## 4. 모든 답에 붙는 엔벨로프

| 칸 | 뜻 |
|---|---|
| `as_of` | 어느 자리까지 반영했나: `kg_id`, KG의 `generation`(시점 복구를 할 때마다 오르는 번호), `change_seq`, 확정 버전 `sealed_version`(있으면). 고정돼 있으면 `pinned`(고정한 버전과 이유) |
| `volatile_as_of` | `volatile` 값을 어느 때의 것으로 줬나: `current`(지금 것) 또는 `omitted`("그 버전 때"의 답이라 주지 않았다) |
| `served_by` | 어느 역할이 답했나(`role`: `api_sql`, `engine`), 엔진이면 프로젝션의 `built_at` |
| `freshness` | 그 답에 쓰인 소스 중 **가장 나쁜 값**: `worst_source_id`, `oldest_age_seconds`, `state`(`fresh`, `stale`, `no_expected_period`). `stale`은 기대 주기의 두 배를 넘긴 것(ADR-0011, 가정) |
| `evidence` | `min_grade`(쓰인 입력 중 가장 낮은 등급), `counts`(`fact`, `assumed`, `unknown`), `kinds`(쓰인 근거의 종류: `configured`, `observed`, `declared`, `derived`), `observation_age_seconds` |
| `result_complete` | `total`, `returned`, `truncated`, `reason`(`limit`, `time_budget`, `size_budget`), `next_cursor` |
| `hidden` | `hidden_passed_count`(답이 지난 가려진 객체의 수)와 `hidden_reached_count`(답이 닿은 끝에 있는 가려진 객체의 수). 범위가 전체가 아닌 주체에게는 정확한 수 대신 있음/없음만 줘요(설정으로 수를 열 수 있어요). 내용은 없다 |
| `health_hint` | 이 답에 쓰인 것 가운데 헬스 상태가 나쁜 것의 수와 목록(ADR-0013) |
| `model_limits` | 이 답이 모르는 것의 문구(14절) |
| `warnings` | 예: `capacity_group_unknown`(용량 묶음을 아직 따지지 않아 알 수 없음으로 줬다) |
| `data` | 질의마다의 본문 |

**고정.** 나쁜 데이터가 들어왔을 때 `operator`(전체 범위)나 `admin`이 "물음은 버전 V로 답하라"고 고정할 수 있어요(`serving_pin`, ADR-0014). 데이터를 되감지 않고 읽는 쪽만 고정하고, 답의 `as_of.pinned`에 표시돼요. 고정하면 질의 엔진이 그 버전을 올려요. 다 올라올 때까지 고정은 `pending`이고, 그동안은 지금 것으로 답하며 `as_of.pinned`에 `pending`이라고 표시해요. 올라온 뒤에 고정이 서요(`active`).

**`volatile` 값의 길.** `volatile` 속성이 바뀌면 이력 없는 **경량 변경 레코드**(객체, 속성, 새 값의 표지, `change_seq`)이 적혀요. 값이 같아 쓰지 않은 것은 줄도 없어요(ADR-0009). 프로젝션과 충족 표시와 변경 알림은 적용된 변경의 기록과 함께 이 줄을 따라요. 줄은 `hist`와 버전의 해시에 들지 않고 짧게 보관돼요([기본값 표](defaults.md)). 그래서 `as_observed`가 읽는 관측 상태가 `volatile`이어도 엔진이 알아요. **"그 버전 때"의 답은 `volatile` 값을 주지 않아요**(지금 것을 섞지 않아요). `volatile_as_of`가 `omitted`로 알려요. **링크는 `volatile`이 될 수 없어요.** 영향 범위가 읽는 링크는 자주 옮겨 다니는 `runs_on`도 보통의 변경이에요(그 쓰기의 양은 측정해서 봐요).

가상의 예시예요. [예시 문서](kg-queries-examples.md) 1절의 `impact` 엔벨로프예요.

```yaml
as_of: {kg_id: "kg-main", generation: 1, change_seq: 90417, sealed_version: null}
volatile_as_of: current
served_by: {role: engine, built_at: "2031-03-04T02:14:30Z"}
freshness: {worst_source_id: "src-facility-sheet", oldest_age_seconds: 5400, state: fresh}
evidence: {min_grade: assumed, counts: {fact: 41, assumed: 1, unknown: 0}, kinds: [configured, declared], observation_age_seconds: null}
result_complete: {total: 3, returned: 3, truncated: false, reason: null, next_cursor: null}
hidden: {hidden_passed_count: 0, hidden_reached_count: 0}
health_hint: {unhealthy_inputs: 0}
model_limits: ["전원이 끊긴 뒤 UPS 배터리가 버티는 시간은 따지지 않는다"]
warnings: []
data:
  facts_only: {down: [srv-2], exposed_direct: [srv-1], exposed_inherited: [vm-1], unknown: [srv-3]}
  with_assumptions: {down: [srv-2], exposed_direct: [srv-1, srv-3], exposed_inherited: [vm-1], unknown: []}
```

## 5. 영향 범위의 뜻

### 5.1 상태는 채널마다, 링크 의미는 채널로

한 객체의 상태는 "살았다/죽었다" 하나가 아니에요. 채널(`service`, `power`, `cooling`, `network`, `storage`, `management`)마다 `up`, `down`, `unknown`이에요. "서버는 켜져 있는데 망에서 끊겼다"와 "서비스는 도는데 관리할 길이 끊겼다"를 가르려는 것이에요. 링크 의미(열하나)은 "출발의 어느 채널이 도착의 어느 채널에 기대나"로 정의돼요. 정의는 ADR-0007이 쥐고, 계산이 쓰는 모양만 적어요.

| 링크 의미 | 계산에서 |
|---|---|
| `runs_on` | 출발의 `service` ← 도착의 `service`. 매개 값 `failover_to`(묶음)가 있으면 5.4절 |
| `requires`(`hard`/`soft`) | `service` ← `service`. soft는 죽지 않고 `degraded` |
| `supplied_by`(`power`/`cooling`/`network`/`storage`) | 출발의 그 채널 ← 도착의 `service` |
| `realized_by` | `service` ← `service` |
| `part_of` | 전체의 `service`가 죽으면 부품도 죽는다. 전체가 부품을 필요로 하는지는 `whole_needs_part`(`hard`/`soft`/`none`, 기본 `none`)나 이중화 그룹으로 |
| `contained_in` | 자리다. 자리 단위의 사고(`site_failure`)에서만 내용물로 번진다 |
| `connected_to` | 이어짐의 간선. 양 끝이 살아 있을 때만 쓴다. `network`의 길을 따질 때 쓴다 |
| `member_of` | 묶음의 구성원(매개 값 무게, 역할) |
| `managed_by` | 출발의 `management` ← 도착의 `service` |
| `owned_by`, `record_only` | 번지지 않는다. 프로젝션에도 넣지 않는다 |

타입은 자기 `service`가 어느 채널을 필요로 하는지를 특성으로 선언해요(`Powered`는 `power`, `Cooled`는 `cooling`, `NetworkAttached`는 `network`, 스토리지를 쓰는 타입은 `storage`를 hard로. 타입이 강도를 바꿀 수 있어요). 링크에는 `delay_class`(`immediate`, `delayed`)가 있어요. 링크는 모두 보통의 변경이라 엔진에 닿아요(`volatile`이 될 수 없어요).

### 5.2 이중화 그룹

`RedundancyGroup`(하위 타입 `Lag`, `HaCluster` 같은 것)은 방식이 둘이에요.

| 방식 | 따지는 법 |
|---|---|
| `count` | 산 구성원이 `min_alive`개 이상이어야 한다 |
| `capacity` | 산 구성원의 용량 합이 기대는 쪽의 수요 합 이상이어야 한다. 수요는 기대는 링크의 속성 `demand`에 적는다 |

모델은 둘 다 갖고, **엔진은 `count`를 먼저 만들어요.** 그 전까지 `capacity` 묶음의 답은 `unknown`이고 `warnings`에 알려요. `count`는 구성원 중 산 것 `a`, 모르는 것 `u`로:

| 조건 | 묶음의 값 |
|---|---|
| `a >= min_alive` | 살았다. `slack = a - min_alive`가 여유다 |
| `a + u < min_alive` | 죽었다 |
| 그 밖 | 모른다 |

모르는 구성원이 있어도 "안전하다"로 읽히지 않아요. 구성원이 산지는 **그 구성원의 의존을 끝까지 따라가서** 정해요. 겉으로 이중화인데 같은 PDU에 매달려 있으면 한꺼번에 죽어요([예시 문서](kg-queries-examples.md) 1절). ECMP, LACP, BGP의 묶음은 질의할 때 계산하지 않고 **받을 때 만들어 저장해요**. 같은 묶음을 설정(`configured`)에서 끌어낼 수도, 경로표나 이웃 표(`observed`)에서 받을 수도, 설계 문서(`declared`)에서 받을 수도 있어요. 길이 여럿이면 다 받아 두고, 설정과 관측이 어긋나면 문제 목록에 올라요. 질의는 `view`로 어느 쪽을 쓸지 골라요. 설정을 온전히 읽은 묶음은 `fact`예요. `min_alive`에도 근거 등급이 있고, 모르면 1부터 `n`까지 가능한 값 모두에서 답이 같을 때만 그 답이에요(예시 문서 5절).

### 5.3 의존과 이어짐, 번갈아 최소 고정점까지

`network` 채널은 둘로 따져요.

| | 의존 | 이어짐 |
|---|---|---|
| 물음 | 죽음이 묶음의 임계값을 넘어 번지나 | 앵커까지 길이 남아 있나 |
| 대상 | 설정에서 끌어낸 묶음(LACP, ECMP, 짝을 이룬 스위치)과 5.1의 의존 | `connected_to` 간선 위의 길. 앵커는 특성 `NetworkAnchor`를 단 객체 |
| 방법 | 죽은 것에서 출발해 묶음마다 산 수를 센다 | 앵커에서 살아 있는 간선을 따라 닿는 것을 찾는다 |
| 틀리는 법 | 순서 있는 의존이면 맞다 | 고리가 있는 망에서 세기만 하면 낙관적으로 틀린다 |

가르는 이유는 **조사 담당의 추론**이에요. 문헌으로 확인한 사실이 아니에요. 고리가 있는 망에서 두 스위치가 서로를 산 이웃으로 세면, 둘 다 위쪽과 끊겨도 서로 때문에 살아 있다고 읽는다는 거예요. 그래서 말로 믿지 않고 **기준 구현과 대조하는 테스트**으로 지켜요(7절).

계산은 **가장 작은 고정점**이에요. 처음에는 모두 살아 있고, 죽었다고 준 것에서 출발해 규칙이 강제하는 것만 죽어요. 의존이 죽인 스위치는 이어짐의 간선에서 빠지고, 닿지 못하는 객체는 다시 의존의 죽은 것에 들어가요. **번갈아 돌려 더 늘지 않을 때 멈춰요.** 죽은 것은 늘기만 하므로 반드시 멈춰요.

이어짐도 세 값이에요. 살아 있는 간선만으로 닿으면 `yes`, 살아 있는 간선과 모르는 간선을 다 써도 못 닿으면 `no`, 그 밖은 `unknown`이에요. **범위 안에 앵커가 없으면 `unknown`이에요.** "안전"이 아니에요.

### 5.4 객체마다의 결과
| 결과 | 뜻 |
|---|---|
| `down` | (가장 무거움) `service`가 죽는다. `lost_channels`가 이유를 말한다. 망만 잃었으면 "끊겨 있다"로 읽힌다 |
| `interrupted` | 죽지만 선언된 넘겨받기(`runs_on`의 `failover_to` 묶음)가 살아 있어 돌아올 것으로 본다. 묶음의 여유를 모르면 `unknown` |
| `degraded` | 살아 있지만 soft로 기대던 것을 잃었거나, 기대는 묶음의 구성원이 줄었다(여유는 남았다) |
| `exposed_direct` | 살아 있지만 자기가 기대는 묶음의 여유가 0이다(자기의 이중화를 잃었다). 하나만 더 죽으면 죽는다 |
| `exposed_inherited` | 살아 있지만 기대는 것이 노출됨이다(이중화를 잃은 랙 위의 VM) |
| `unmanageable` | `service`는 살아 있고 `management`만 잃었다 |
| `unknown` | 모르는 입력 때문에 정할 수 없다 |

한 객체에 결과가 여럿 걸리면 가장 무거운 것 하나를 `result`로 주고 나머지는 `also`에 적어요. 무거운 차례는 `down` > `interrupted` > `unknown` > `exposed_direct` > `exposed_inherited` > `degraded` > `unmanageable`이에요.

**노출됨은 끝까지 번져요. 깊이를 정하는 설정은 없어요.** 같은 KG의 같은 물음이 배포마다 다른 답을 내면 안 되고, 기준 구현과 골든 값이 깊이마다 따로 들면 안 돼서예요. 답은 직접 노출과 상속된 노출을 가르고 `counts_by_result`도 갈라요. 번진 만큼 질의가 닿는 노드가 늘어요(12절).

시간은 따지지 않아요(냉방을 잃고 버티는 시간, 배터리가 버티는 시간, 넘겨받는 데 걸리는 시간). 링크의 `delay_class`가 `delayed`면 답에 "늦게 나타남"(`delay_class`)으로 표시만 해요. 버티는 시간 같은 값은 속성으로 받아 둬요.

### 5.5 여러 개가 함께 죽는 물음, `exposure`, 최소 절단
`dead`는 객체 ID의 목록이에요. 시드를 합쳐서 한 번에 따져요. 죽음은 늘기만 하므로(단조) 시드의 순서에 답이 달라지지 않아요. `dead_links`는 죽은 링크, `site_failure`는 자리 단위의 사고(`contained_in`으로 안의 것이 죽는다), `dead_scope_id`는 저장해 둔 범위 전체예요.

`exposure`는 `dead`가 주어진 상태에서 **살았고 여유(`slack`)가 0인 묶음**을 골라요. 그 묶음의 산 구성원이 후보예요. 후보마다 "이것까지 죽으면" 새로 `down`이 되는 객체 수(`would_die_count`)를 내요. 후보는 모든 객체가 아니라 빡빡한 묶음의 구성원과 그 의존의 위쪽이라 작아요. `dead`가 비어 있으면 지금 이미 여유가 0인 묶음의 목록이에요. 묶음이 아닌 단일 의존은 모든 객체에 있으므로 대상이 아니에요.

`target_object_id`를 주면 **최소 절단**("무엇 둘셋이 함께 죽으면 끊기나")을 세요. 일반적으로 어려운 문제라(하위식을 공유하면 NP-hard라고 조사 담당이 일반 지식으로 적었고 문헌은 열람하지 못했어요) **작은 차수까지만** 세요. 기본 2, 상한 3은 가정이에요. 후보는 공통 조상을 공유하는 쌍으로 좁혀요. 답의 `cut_complete_up_to`가 어느 차수까지 다 셌나를 적어서, 세지 않은 차수가 "없다"로 읽히지 않게 해요.

### 5.6 사실만 쓴 답, 가정 포함 응답, 알 수 없음

| 답 | 입력을 읽는 법 |
|---|---|
| 사실만 쓴 답(`facts_only`) | `fact` 입력만 쓴다. `assumed` 입력은 **알 수 없음으로 읽는다**(없는 것으로 보지 않는다). `min_alive`가 모르면 가능한 값 모두에서 같을 때만 그 답 |
| 가정 포함 응답(`with_assumptions`) | `assumed` 입력도 `fact`처럼 쓴다 |
| 알 수 없음(`unknown`) | 두 답 어디서든 `unknown` 입력 때문에 정할 수 없는 객체. 따로 준다 |

사실만 쓴 답에서 정해진 것은 가정 포함 응답에서도 같아요. 알 수 없음은 가정을 넣으면 정해질 수 있어요. 7절이 불변식으로 걸어요. 고리(자기가 관리하는 클러스터 위에서 도는 제어부 같은)는 최소 고정점이라 **스스로 죽지 않아요.**

### 5.7 `impact`의 답

| 칸 | 뜻 |
|---|---|
| `facts_only`, `with_assumptions` | 각각 결과별 목록(`down`, `interrupted`, `degraded`, `exposed_direct`, `exposed_inherited`, `unmanageable`, `unknown`). 먼저 `counts_by_result`, `counts_by_type`, `counts_by_parent`(자리), 그다음 `items` |
| `items` | `object_id`, `object_type`, `result`(가장 무거운 것), `also`(나머지), `lost_channels`, `delay_class`, `via`(직전 원인 하나), `data_health`(쓰인 데이터가 이상 상태면 표시) |
| `groups_touched` | 영향을 받은 묶음(`group_id`, `mode`, `value`, `alive`, `unknown`, `n`, `min_alive`, `slack`). 읽을 수 없는 묶음은 나오지 않고 `hidden`으로만 잡혀요 |
| `reachability` | 범위 안의 앵커 수, 닿지 못하게 된 객체 요약. 앵커가 없으면 `network`는 `unknown` |

`exposed_direct`, `exposed_inherited`, `degraded`는 **시드 때문에 달라진 객체**에만 나와요. 원래 여유가 0인 묶음은 `exposure`가 맡아요. `max_staleness`를 넘은 관측은 **쓰지 않고** 그 입력을 알 수 없음으로 읽어요. 설계 기준(`as_designed`)에 입력이 있으면 그것으로 물러나요. 답의 `stale_inputs`와 `warnings`(`stale_observation_not_used`)가 그렇게 했다고 알려요. 영향 범위가 이상 상태인 데이터(소스가 멈춤 등)에 기대면 `data_health`로 알려요(ADR-0013).

## 6. 손으로 따라가기

전원, LAG, HA 클러스터, MLAG, N+1 냉방의 가상의 예시 다섯은 [영향 범위를 손으로 따라가기](kg-queries-examples.md)에 있어요. 사실만 쓴 답과 가정 포함 응답이 갈리는 곳, 의존만 쓰면 틀리는 곳, 직접 노출됨과 상속된 노출됨이 나뉘는 곳을 보여요.

## 7. 기준 구현과 속성 기반 테스트

**기준 구현**은 느리지만 뻔한 계산이에요. 모든 객체를 "산 것"으로 놓고, 시드를 죽은 것으로 바꾼 뒤, 정의대로 모든 객체의 채널 값을 다시 구하는 것을 더 바뀌지 않을 때까지 되풀이해요(최소 고정점, 세 값). 이어짐은 앵커에서 하는 너비 우선 탐색이에요. 엔진은 빨라야 하므로 다르게 만들고, **엔진의 답 = 기준 구현의 답**이어야 해요. 기준 구현은 `core.query`의 일부이고 엔진 포트의 계약 테스트로 함께 가요. 엔진을 새로 짜거나 크게 고칠 때도 같은 골든 파일과 같은 테스트를 통과해야 해요. **기준 구현에는 설계의 뜻 전부가 들어가요**: 세 값, 두 답, 의존과 이어짐의 번갈아 돌리기, 채널 여섯의 동시 상태, 직접·상속된 노출됨, 근거와 본 때와 `max_staleness`, `volatile` 줄을 받는 관측. 영향 범위 단계의 첫 일이 이 기준 구현이고, 합성 세계 생성기의 정답을 이것으로 만들어요([엔진의 모양](query-engine-shape.md)).

| 불변식 | 뜻 |
|---|---|
| 엔진 = 기준 구현 | 임의로 만든 회로(의존, 묶음, 이어짐)와 고리가 있는 망, 합성 세계에서 답이 같다 |
| 단조 | 시드를 더하면 `down`인 것이 줄지 않는다. `unknown`과 `exposed_direct`·`exposed_inherited`는 단조가 아니다 |
| 죽은 것을 더함 | 이미 죽은 것을 시드에 더해도 답이 같다. 시드의 순서와 객체 번호의 순열에 답이 달라지지 않는다 |
| 스스로 죽지 않음 | 시드가 없으면 아무것도 죽지 않는다. 의존의 고리 모양 입력도 같다 |
| `view` | `as_designed`의 답은 관측 입력이 바뀌어도 변하지 않는다 |
| `min_alive` | 낮추면 `down`인 것이 늘지 않는다. 1이면 "하나라도", `n`이면 "모두"와 같다 |
| 구성원 | 산 구성원을 더하면 `down`인 것이 늘지 않는다 |
| 사실과 가정 | 사실만 쓴 답에서 정해진 것은 가정 포함 응답에서도 같다 |
| 고리 | 고리가 있는 무방향 망에서 이어짐이 너비 우선 탐색과 같다(6.4의 오답 모양을 포함). 앵커가 없으면 `unknown` |
| 프로젝션 | 같은 변경 시퀀스이면 같은 프로젝션(내용 해시가 같다). 변경을 늘어난 만큼만 고친 프로젝션 = 통째로 다시 만든 프로젝션 = 조각마다 합친 프로젝션 |
| 합치기와 바꾸기 | 조각을 합치거나 판을 바꾸는 중의 답은 앞 판의 답이거나 뒤 판의 답이다. 섞이지 않는다 |
| 노출됨 | 상속된 노출됨은 뿌리를 거슬러 가면 언제나 직접 노출됨에 닿는다. 번지는 깊이로 답이 갈리지 않는다 |
| 답의 칸 | `as_of`가 있고, `truncated`가 참이면 `next_cursor`나 이유가 있다 |

## 8. 프로젝션

메모리 안의 그래프예요. 파생물이라 다시 만들 수 있고 고가용의 대상이 아니에요. 질의 엔진이 올려 영향 범위와 `exposure`와 길을 답해요. **워커가 만든 처음 판**에 **변경 시퀀스**을 받아 늘어난 만큼만 고쳐요.

| 들어가는 것 | 뜻 |
|---|---|
| 객체 표 | 안정 ID(16바이트)와 내부 번호(32비트)의 대응, 타입 번호(`type_no`), 물러남 표시. 내부 번호는 벌마다 달라서 답과 커서에 나가지 않아요 |
| 이름과 타입의 작은 표 | 표시 이름과 타입 이름(저장소가 멈춰도 답에 이름이 붙게). **민감한 속성은 넣지 않는다** |
| 뜻별 이웃 배열 | 링크 의미(`runs_on`과 `failover_to`, `requires`의 `hard`/`soft`, `supplied_by`의 채널별, `realized_by`, `part_of`와 `whole_needs_part`, `member_of`, `contained_in`, `connected_to`, `managed_by`)마다 정방향과 역방향. 링크마다 근거 등급, 근거의 종류, 본 때, `delay_class` |
| 묶음의 표 | 묶음 ID, 구성원, 방식(`count`, `capacity`), `min_alive`와 그 등급, 무게, 기대는 링크의 `demand` |
| 특성 표 | 객체의 특성(`Powered`, `NetworkAnchor` 등)과 필요로 하는 채널의 강도 |
| 읽기 권한용 표 | 객체의 타입 번호, 객체마다 `scope_keys`(타입에 `scope_key: true`로 선언한 속성. 타입마다 넷까지) 속성의 값, 속성의 민감 등급. API가 답을 거를 때 쓴다(ADR-0012) |
| `volatile` 관측 상태 | `as_observed`가 읽는 관측 상태(올라와 있나 같은 것)를 객체마다 쥐고, 경량 변경 레코드를 따라 고쳐요 |
| 목록 머리 | 소스마다 `last_complete_at`과 기대 주기(신선도를 엔진 혼자 계산하려고) |

**파일 형식의 요구.** 특정 언어의 직렬화에 묶이지 않아요. 평평한 배열과 머리(형식 버전, 반영한 `change_seq`, 그래프 해시, 섹션 표, 섹션마다와 전체의 내용 해시)로 이루어져요. 해시는 정규 바이너리 인코딩 위의 SHA-256이에요(ADR-0006). 순서는 모두 정해진 순서로 정렬해요. **자리(컨테인먼트) 단위의 조각으로 나눠 올릴 수 있어야 해요.** 조각마다 해시가 있고, 조각을 건너는 간선은 두 조각의 **경계 표**에 둬요. 열 형식은 구현하는 변경에서 정해요. 파일 보관의 쓰기는 워커만, 읽기는 엔진 역할만 해요(프로젝션에 이름과 `scope_keys` 값이 들어 있어요). 새로 뜨는 엔진이 올릴 프로젝션이 없으면 저장소에서 직접 지어요(파일 보관은 빠르게 뜨려는 것이고 없으면 안 되는 것이 아니에요).

**늘어난 만큼만 고치는 흐름.** 바탕 배열(조각마다 바뀌지 않는 판)과 덧붙임 겹(더한 간선은 출발 객체별 목록, 지운 간선은 묘비 표시)으로 이루어져요. 읽는 쪽은 (바탕 판, 겹의 `change_seq` 자리)를 쥐고, 겹은 `change_seq` 순서로 추가만 하므로 읽기에 잠금이 필요 없어요. 엔진은 프로젝션으로 가는 변경과 경량 변경 레코드를 포트로 받아요. 겹이 쌓이면 질의가 느려져서(1억 단위에서 간선의 1% 남짓이 쌓이자 홀 하나가 서는 물음의 가운데값이 3ms에서 14ms로 올랐어요) 합치는 **주기의 예산**을 둬요.

**통째로 다시 합치지 않아요.** 합치는 순간의 메모리가 평소의 약 2.4배라서, 바탕을 조각으로 나눠 **조각마다 합치고 버전의 바꿈도 조각 단위로** 해요. 엔진의 메모리 예산은 "평소 + 가장 큰 조각의 합치기"예요([엔진의 모양](query-engine-shape.md)). 통째로 다시 지어 갈아 끼우는 것은 초기 적재와 차이가 임계값을 넘는 정의의 병합뿐이에요. 보통의 병합 결과는 보통의 변경 배치로 흘러요. 갈아 끼울 때 프로젝션 파일은 워커가 만들어 **파일 보관 포트**에 올려요(어댑터: 파일 시스템이나 객체 저장소. PostgreSQL에 넣지 않는다). 엔진은 받아 자기 디스크에 두고 해시를 확인해 올린 뒤, 새 질의를 새 쪽으로 돌려요. 진행 중이던 질의가 끝나면 옛 것을 놓아요. 한 벌씩 차례로 해요.

## 9. 질의 엔진이라는 역할

ADR-0006의 역할 넷(API, 워커, 질의 엔진, 수집기) 중 하나예요. 같은 실행 파일에서 시작점만 달라요.

- **여러 벌.** 부하에 따라 벌 수를 늘려요. 각 벌이 자기 메모리에 KG마다의 프로젝션을 들어요.
- **저장소가 멈춰도 답한다.** 올려 둔 프로젝션으로 답을 잇고, 엔벨로프에 `as_of`와 신선도(목록 머리의 시각으로 계산)가 적혀요. 새 변경은 받지 못하니 옛 자리의 답인 것이 보여요.
- **부르는 쪽은 엔진을 직접 부르지 않고 언제나 API를 거쳐요.** API는 저장소가 멈춰도 묻기의 길을 답해요. 로그인의 토큰은 저장소 없이 확인하고, 그룹과 역할의 짝은 나이에 상한이 있는 사본을 써요(ADR-0011, ADR-0012).
- **엔진의 경계**는 프로젝션 파일과 변경 시퀀스예요. 엔진은 그 둘(경량 변경 레코드 포함)만 읽고 정해진 계약으로 답해요. 엔진은 Go로 직접 만들어요(확정. ADR-0006, [엔진의 모양](query-engine-shape.md)). 이 경계는 엔진을 따로 테스트하고 고치려는 것이고 언어를 바꾸려는 것이 아니에요.
- **API와 엔진 사이의 계약.**

| 방향 | 칸 |
|---|---|
| API → 엔진 | KG의 ID와 세대, 질의 이름, 인자(`view`, `max_staleness` 포함), `kg_version_id`(없으면 지금), 시간·크기 예산, 요청 ID |
| 엔진 → API | 엔벨로프와 본문(객체 ID, 타입, 등급을 담아서 API가 읽기 권한으로 거를 수 있게), 또는 오류(`version_not_loaded`+들고 있는 범위, `budget_exceeded`+좁히는 법, `too_many_seeds`) |
| 엔진 → 조율 | 반영한 `change_seq`, 준비 상태, 프로젝션의 `built_at` |

- **엔진이 올려 두는 버전.** 지금 것과 고정된(`serving_pin`) 버전만이에요. 그 밖의 옛 버전의 `impact`는 요청하면 따로 올려 답하고, 올리는 동안은 `version_not_loaded`(들고 있는 범위 포함)로 답해요. 고정을 건 버전은 따로 요청하지 않아도 엔진이 올려요. `find`, `get`, `changes` 같은 SQL 질의는 옛 버전도 바로 읽어요.
- **무엇이 SQL로 가고 무엇이 엔진으로 가나.** 기본 갈래는 이래요. `find`, `get`, `changes`, `versions`, `quality`, `health`, `scopes`, `containment`, `competency`(표시를 세는 것)는 SQL, `neighbors`는 1~3걸음까지 SQL, `paths`, `impact`, `exposure`는 엔진이에요. 어디까지가 SQL로 되는지는 지어낸 규모에서 SQL(재귀)로도 구현해 측정해서 가려요. SQL이 목표를 맞추면 엔진의 범위가 줄 수 있어요. 개발 환경에서는 한 프로세스에 역할을 함께 띄워도 포트를 거쳐요.

## 10. 면 셋

### HTTP API

- **KG는 계약의 자원이에요.** KG에 든 것(객체, 링크, 질의, 변경, 버전, 충족률, 헬스 상태, 재수집)은 `/v1/kgs/{kg}/...`이고, 정의와 권한 부여와 시크릿과 운영은 배포에 하나라 `/v1/...` 바로 아래예요. 정의의 메인 브랜치는 배포에 하나이고 KG는 그 메인 브랜치의 어느 정의 세트를 쓰는지 가리켜요. 권한 부여의 범위에 KG를 고르는 칸이 있어요(기본 `main`).
- **버전이 있는 계약.** 호환성을 호환성을 깨는 변경은 새 버전으로 내요. 계약은 `core.contract`가 쥐고, 길의 규칙과 목록은 [REST API 설계](rest-api.md)(ADR-0016)가 정해요.
- **서비스 계정.** 사람이 아닌 시스템이 쓰는 계정이 같은 역할(ADR-0012)을 받아요.
- **부르는 쪽마다 한도.** 초당 요청 수, 동시 요청 수, 계산 예산(비용 등급별)을 계정마다 정해요. 기본값은 측정해서 정해요. 넘으면 오류가 기다릴 시간이나 좁히는 법을 알려요.
- **대량으로 읽기(`bulk_read`).** 범위 하나(`scope_id`)를 한 버전에서 통째로 읽는 경로예요. 낱개 질의를 수만 번 부르게 두지 않으려는 것이에요. 쪽 단위 흐름으로 내려받고, 같은 엔벨로프가 붙고, 가림이 적용되고, 쓴 사람이 감사에 남아요(ADR-0012). 열셋 질의가 아니라 따로 둔 길이에요.
- **쓰임 기록.** 누가 어느 타입과 속성을 읽었는지를 서비스 계정마다 API가 남겨요(보통의 읽기는 감사 로그가 아니라 쓰임의 통계예요. 민감한 속성과 대량으로 읽기는 감사에 남아요. ADR-0012, ADR-0013). 정의의 계보가 "누가 쓰나"를 답하는 데 써요.
- **쓰는 경로 둘.** 저장해 둔 범위는 정의가 아니에요. 보기 역할이 API로 만들고, 만든 그룹의 것이에요. KG가 틀렸다는 **문제를 알리는 길**도 API에 있고 문제 목록으로 들어가요(ADR-0011). 둘 다 KG를 바꾸지 않아요.

### 읽기 전용 MCP

- 툴 목록은 사전 정의 질의 13종과 같은 이름이에요(`find` ... `scopes`, `health`, `competency`). 여기에 **재수집**(지정해서 읽기)와 **기대와 대조**를 읽는 면으로 더해요(ingestion-extensions.md). `bulk_read`는 에이전트에게 주지 않아요. **KG의 MCP는 읽기만** 해요(운영의 MCP 인터페이스에는 행위가 있어요. self-observability.md). 현실의 시스템과 정의를 바꾸지 못하고, 범위를 만들거나 문제를 알리는 길도 없어요. 재수집은 소스에서 읽는 일이지만 같은 수집 길로 적용되므로 작업(operation)을 만들어요. 그 작업을 만들 수 있는 역할은 15절에 열려 있어요. 툴 설명에 이 뜻을 밝혀요.
- 답의 크기에 **상한**이 있고 넘으면 잘라서 더 좁게 묻는 법을 안내해요. 상한의 숫자는 가정이고 측정해서 정해요.
- **간단히와 자세히**(`detail`: `brief`, `full`). 기본은 간단히예요. 사람이 읽는 이름과 짧은 ID를 함께 줘요.
- **오류가 다음에 할 일을 말한다.** 예:

```yaml
error: {code: ambiguous_name, message: "이름이 같은 객체가 3개예요",
        next_step: "object_type을 더해서 다시 물어요",
        candidates: [{object_id: "obj-31", object_type: "PhysicalServer", display_name: "srv-31"}]}
```

### 변경 알림

- **알리는 것.** 변경에서 바뀐 것: 객체와 링크의 더함, 고침, 물러남, 그리고 묶음(`min_alive`)의 변화. 변경의 기록에서 나오고, `volatile` 속성의 바뀜은 가벼운 줄을 따라 이력 없는 알림으로 나가요. 데이터 헬스 상태의 사건도 이 길로 낼 수 있어요(ADR-0013).
- **가져가기 방식을 먼저.** 변경 목록은 `changes`와 같은 모양이에요. 받는 쪽이 마지막으로 가져간 `change_seq`를 `from_change_seq`로 주면 그다음부터 줘요. 읽기 권한은 목록에도 적용돼요(타입별 수도 가려진 것을 뺀 수예요).
- **밀어 주기**는 뒤예요. 보내는 경로는 포트 뒤에 두고, 받는 쪽마다의 필터링과 전달 보장은 그때 정해요.

## 11. 읽기 권한과 가려진 것

권한의 규칙은 [읽기 권한과 역할](access-control.md)과 ADR-0012가 정해요. 읽기는 권한 부여의 범위(객체 타입, 민감 등급의 한도, 클로즈드 베타 전에는 객체의 조건)예요. 조건에 맞지 않는 객체는 가려진 객체와 같이 다뤄요. 여기서는 영향 범위에만 닿는 것만 적어요.

- **계산은 전체 그래프에서 한다.** 부르는 쪽에 따라 계산이 달라지면 결과가 틀려요. **엔진의 답을 API가 거르는 것**이 읽기 권한이에요(프로젝션의 읽기 권한용 표를 써서요).
- 영향 범위가 읽을 수 없는 객체를 지나면 `hidden.hidden_passed_count`에, 영향받는 끝에 닿으면 `hidden.hidden_reached_count`에 잡혀요. 범위가 전체가 아닌 주체에게는 정확한 수 대신 있음/없음만 줘요(설정으로 수를 열 수 있어요). 그 객체는 결과 목록에 나오지 않고 내용도 타입도 주지 않아요. `via`는 "가려진 객체"로 적혀요.
- **거른 뒤의 쪽은 거른 수로 채워요.** API가 엔진의 쪽을 거르고 남은 것이 `limit`보다 적으면 엔진에 더 물어 `limit`까지 채워 돌려줘요(`result_complete.total`은 거른 뒤의 값).
- **수를 내는 모든 칸은 가려진 것을 뺀 수예요.** `counts_*`, `total`, `groups_touched`, `would_die_count`, `subtree_size`, `link_counts`, `neighbors`의 `groups`, 충족률의 분모, 변경 알림의 타입별 수가 그래요. 같은 타입 안의 남의 객체 수가 곧 정보일 수 있어서예요. 계약에 그렇게 적어요.
- `paths`가 가려진 객체를 지나면 그 걸음은 "가려진 객체 하나"로 보여요(걸음 수는 드러나요).
- **물어서 캐는 길은 다 막지 못해요.** `impact`와 `paths`는 죽은 것(`dead`, `dead_links`)을 부르는 쪽이 골라요. 보이는 객체를 하나씩 죽여 보며 `hidden`의 있음/없음이 바뀌는 것을 보면 가려진 쪽의 의존을 어림할 수 있어요. 정확한 수를 주지 않는 것이 첫 막이에요.
- 가려진 속성은 "가려짐"으로 보이고, 그것을 찾는 조건으로 쓰면 거절해요. 읽을 수 없는 객체의 ID를 `dead`에 넣으면 "찾을 수 없음"과 같은 모양으로 답해요.

## 12. 성능

요구는 **제품 책임자의 숫자**예요. 규모의 요구는 한 KG에 수천만 개, 설계의 여유는 수억까지예요(REQ-134).

| 물음 | 요구 | 근거의 상태 |
|---|---|---|
| 스위치의 ECMP·BGP·LACP를 다 고려한 영향 범위 | 1초 안에 | **아직 근거가 없는 목표예요.** 이어짐 계산을 넣은 모양으로 측정하지 않았어요 |
| 설비 문제가 VM·앱·서비스에 미치는 범위 | 수 초 안에 | 지어낸 모양의 프로토타입에서 자릿수로는 이 안이에요(가정) |

넘겨받은 "3초"는 쓰지 않아요. ECMP·BGP·LACP는 경로표가 아니라 장비 설정 값에서 받을 때 만든 묶음으로 따져요.

**측정한 것.** 같은 엔진의 프로토타입을 지어낸 그래프(노드와 간선 1억 개까지, 2코어 8GB)에서 측정했어요. 그 위는 추정이에요. 홀 하나가 서는 경우의 가운데값은 약 2ms이고, 응답 시간은 KG의 크기가 아니라 질의가 닿는 노드의 수를 따라요. **이 수에 들지 않은 것**(세 값, 두 답, 이어짐의 번갈아 돌리기, 링크마다의 근거와 본 때, 속성 필터링, 과거 버전, 읽기와 쓰기의 동시, 입출력 등)과 메모리가 12바이트의 몇 배가 된다는 것은 [엔진의 모양](query-engine-shape.md) 4절에 있어요. 그래서 위 숫자를 채웠다고 말하지 않아요.

**남은 측정.** 영향 범위 단계의 첫 일은 설계의 뜻 전부를 넣은 기준 구현이고(7절), 그 위에서 다시 재요.

| 순서 | 잴 것 |
|---|---|
| 1 | SQL만으로 답하는 질의의 기준선(재귀로 구현한 영향 범위 포함). 프로젝션이 필요한 범위를 숫자로 정한다 |
| 2 | 저장소에서 프로젝션 만들기: 시간과 메모리 |
| 3 | 영향 범위의 응답 p50/p95/p99. 허브, 깊은 체인, 고리, 큰 영향 범위, 묶음이 많은 입력, 사이트를 건너는 의존, 동시 호출을 넣는다 |
| 4 | 조각 합치기: 가장 큰 조각의 합치기 메모리, 진행 중 질의에 주는 영향, 오버레이가 쌓일 때의 지연(합치는 주기의 예산) |
| 5 | 응답 크기(대표 MCP 호출이 상한 안에서 요약되나)와 결정성(순서를 섞고 워커 수를 바꿔도 같은 프로젝션, 같은 답) |

저장소 쪽(순서 1, 2)의 수천만 측정은 2코어 8GB로는 모자라서 클로즈드 베타 앞에 빈 장비 하나가 필요해요. 안 되면 먼저 파라미터, 색인, 나눔을 고쳐 보고, 그래도 안 되면 제품 책임자에게 **측정한 숫자와 함께** 말해요. 숫자를 슬그머니 바꾸지 않아요.

## 13. 큰 KG를 보이는 일

화면은 다른 저장소의 일이에요(ADR-0016). 여기서는 API가 **묶음과 수를 먼저 주는 것**만 정해요. `neighbors`의 `groups`, `containment`의 `children_summary`와 `subtree_size`, `impact`의 `counts_by_result`, `counts_by_type`, `counts_by_parent`가 그것이에요.

## 14. 알려진 한계와 가정

- 시간은 따지지 않아요(UPS와 냉방이 버티는 시간, 넘겨받는 시간, 수렴 시간). `delay_class` 표시만 해요. 부하가 옮겨 가는 연쇄 과부하, 부분 성능 저하, 해시로 나뉘는 ECMP의 쏠림도 따지지 않아요. 엔벨로프의 `model_limits`에 문구가 붙어요.
- 구조와 상태는 KG에 있고 관측의 나이가 답에 붙어요. 수치의 시계열은 KG에 쌓지 않아요. 소프트웨어 버그나 설정 오류는 모델링하지 않아요.
- 엔진은 저장소가 멈추면 새 소스의 신선도를 모르고 마지막으로 받은 값으로 계산해요. 실제보다 최신으로 보일 수 있어요. 고리에서 임계값 세기가 낙관적으로 틀린다는 말은 추론이고, 기준 구현과의 대조가 이를 확인하는 방법이에요.
- 숫자(한도, 차수, 시드 수, 걸음 수, 합격선, 메모리 배수)는 모두 측정하지 않은 가정이에요.
- `volatile` 값에는 이력이 없고 "그 버전 때"의 답에도 없어요. 값이 같아 쓰지 않은 줄 때문에 신선도는 간격의 절반만큼 거칠어요(ADR-0009).
- 가려진 것의 수를 막아도 `dead`를 골라 가며 묻는 길은 완전히 막지 못해요(11절).

## 15. 미결 사항

- 옛 버전의 `impact`를 따로 올리는 데 걸리는 시간과, 한 번에 올려 둘 수 있는 옛 버전의 수.
- 노출됨을 물려주는 링크 의미의 목록(5.1의 표에서 어느 뜻이 `exposed_inherited`를 번지게 하나).
- 조각의 크기와 개수, 합치는 주기의 예산 값(가정으로 기본값 표에 둬요). 재수집 작업을 MCP에서 만들 수 있는 역할.
- 프로젝션 파일의 열 형식. 부르는 쪽마다 한도의 숫자, MCP 답의 크기 상한. 밀어 주기의 형식과 필터링. 질의 언어를 열지는 사전 정의 질의가 못 덮는 물음이 쌓인 뒤에 다시 봐요.

## 16. 관련 결정과 요구사항

- ADR-0010(이 문서의 결정), ADR-0015(`competency`), ADR-0016(REST API), ADR-0006(역할 넷, 정규 인코딩 해시), ADR-0007(채널, 링크 의미, 이중화 그룹, 근거 등급), ADR-0009(변경 시퀀스, 확정 버전, 저장소), ADR-0011(가용성의 등급), ADR-0012(읽기 권한, 감사), ADR-0013(헬스 상태, `quality`, `health`), ADR-0014(정의의 계보).
- REQ-134(규모), REQ-140, REQ-141, REQ-142, REQ-143, REQ-144, REQ-145, REQ-146.
- 이웃 문서: [엔진의 모양](query-engine-shape.md), [영향 범위를 손으로 따라가기](kg-queries-examples.md), [REST API 설계](rest-api.md), [역량 질문](competency-questions.md), [KG 모델](kg-model.md), [KG 저장과 버전](kg-storage.md), [KG 운영](kg-operations.md), [데이터 관측성](data-observability.md), [읽기 권한과 역할](access-control.md).
