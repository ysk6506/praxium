---
title: KG 질의 — 사전 정의 질의, 영향 범위, 프로젝션과 엔진
kind: guide
status: active
follows:
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
---
# KG 질의

KG에 질의하는 경로 전체의 설계예요. 사전 정의 질의 13종, 모든 응답에 붙는 엔벨로프, 영향 범위의 의미, 영향 범위를 계산하는 프로젝션과 질의 엔진, 외부에 공개하는 인터페이스 3가지를 정해요. 코드는 아직 없어요(백엔드는 Go로 정해졌어요. ADR-0006). 타입 이름은 영어이고, 표준 모델의 최종 이름은 ADR-0007의 표준 모델을 따라요. 이 문서의 이름은 예시예요. **질의는 KG 하나 안에서 실행돼요.** KG에 든 데이터를 조회하는 경로는 모두 `/v1/kgs/{kg}/...` 아래에 있고, KG 사이를 가로지르는 질의는 없어요.

## 1. 목적과 다루는 질문

- 누가 KG에 어떤 질의를 어떤 형식으로 할 수 있나. 객체에 장애가 나면 무엇이 어떻게 영향받는지를 이중화, 연결성, 알 수 없는 입력까지 어떻게 정확히 계산하나.
- 질의하는 클라이언트가 많아도, 스토리지가 멈춰도 응답이 이어지게 하려면 무엇을 분리하나. 제품 책임자가 정한 숫자(1초, 수 초)는 아직 근거가 없는 목표예요. 무엇을 측정해서 확인하나(12절).

## 2. 개요

```
 사람·시스템 ─┐                          ┌ SQL: find get neighbors(얕게) containment changes
 에이전트(MCP) ┼ API ─ 사전 정의 질의 13종 ─┤      versions quality health scopes competency
 다른 시스템 ──┘   + 엔벨로프 + 읽기 권한 필터링 └ 엔진: impact exposure paths
 변경 시퀀스 ─▶ 질의 엔진(여러 인스턴스): 메모리의 프로젝션을 증분 갱신한다
 (호출자는 엔진을 직접 호출하지 않는다. 스토리지가 멈춰도 로드해 둔 프로젝션으로 응답한다)
```

## 3. 사전 정의 질의 13종

HTTP 경로는 [REST API 설계](rest-api.md)가 정하고(`/v1/kgs/{kg}/queries:<질의 이름>`. `{kg}`는 KG의 ID이고 운영 KG는 `main`으로도 지정할 수 있어요), 여기서는 질의의 이름과 필드로 적어요. 필드 이름은 계약 유닛(`core.contract`)이 그대로 쓸 이름이에요.

### 공통 입력

| 필드 | 설명 |
|---|---|
| `kg_version_id` | 조회할 KG 버전의 ID("그 버전 시점"). 없으면 "현재"(변경 시퀀스의 마지막). 어느 변경 번호까지 반영했는지가 엔벨로프에 기록된다 |
| `view` | 조회 기준. `as_designed`(`configured`와 `declared`만) 또는 `as_observed`(관측이 있으면 관측, 없으면 설계. 기본) |
| `min_seq`, `wait_timeout` | 적어도 이 변경 번호까지 읽을 수 있게 된 응답을 요구해요. 대규모 적용은 배치가 끝날 때만 읽기 워터마크(`readable_through`)가 올라가서 일부만 반영된 배치를 읽지 않고, `serving_pin`도 이 워터마크를 기준으로 해요. 응답에 변경 번호가 포함돼요. 복제본이 따라오지 못하면 주 스토리지로 우회하거나 `wait_timeout`까지 기다리거나 거절해요(ADR-0009, postgres-operations.md). 세대(`as_of.generation`)가 바뀌었으면 `conflict.generation_changed`로 응답해요. 기대와 대조(`confirmed`, `not_yet`, `contradicted`, `unverifiable`)는 ingestion-extensions.md 3절 |
| `max_staleness` | **소스에서 관측한 값이 얼마나 오래돼도 되는지** 하나만 의미해요. 이보다 오래된 속성 값과 관측은 쓰지 말라는 한도예요. 영향 범위는 한도를 넘은 관측을 **알 수 없음으로 읽고** 설계 기준(`as_designed`)으로 폴백하며, 그렇게 했다고 응답에 표시해요(5.7절). 복제가 따라오기를 기다리는 것은 이 필드가 아니라 `min_seq`와 `wait_timeout`이에요 |
| `limit`, `cursor` | 페이지네이션. `limit`의 기본 100, 상한 1천은 기본값(설정)이다. `cursor`는 객체 ID(엔진의 내부 번호가 아니다), 정렬 키, `change_seq`, KG의 세대를 포함해서, 페이지 도중에 변경이 들어와도 같은 위치부터 이어지고, 엔진 인스턴스가 바뀌어도 이어진다 |
| `shape` | `summary`(수와 그룹 먼저, 기본) 또는 `items`. MCP에서는 `detail`로 `brief`와 `full`을 고른다 |

### 질의별 입출력

| 질의 | 입력 필드 | 출력 필드 | 한도와 페이지네이션 |
|---|---|---|---|
| `find` | `object_type`(`include_subtypes`), `trait`, `attributes`(이름, 연산, 값의 목록. 연산은 `equals`, `in`, `prefix`, `range`), `identifier`(종류, 값), `text`, `scope_id` | `counts_by_type`, `items`(`object_id`, `object_type`, `display_name`, `matched_on`) | 인덱스로 응답한다. `limit`/`cursor`. 읽을 수 없는 타입의 객체는 나오지 않는다. 숨김 속성을 검색 조건으로 쓰면 거절한다 |
| `get` | `object_id`나 `identifier`, `include`(`attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`) | 객체, 속성(`name`, `value`, `unit`, `grade`, `basis`: 근거의 종류, `seen_at`, `stale`)과 가장 나쁜 신선도, 식별자들, 유효한 버전 구간, `provenance_refs`(원본 레코드의 참조와 규칙의 버전) | 한 객체. 링크는 개수만 반환한다(`neighbors`로 읽는다) |
| `neighbors` | `object_id`, `link_types`, `meanings`, `direction`(`out`, `in`, `both`), `max_steps`, `peer_types` | `groups`(`link_type`, `meaning`, `direction`, `peer_type`, `count`)를 먼저, 그다음 `items`(`link_id`, `link_type`, `peer_object_id`, `peer_type`, `grade`) | `max_steps` 기본 1, 상한 3은 가정. 허브는 `groups`만 반환하고 `items`는 `limit`로 자른다 |
| `containment` | `object_id`, `direction`(`up`, `down`), `depth`, `child_types` | `ancestors`(건물, 방, 랙의 순서), `children_summary`(타입별 수), `subtree_size`, `items` | 깊이 기본 1. 자식은 페이지네이션 |
| `paths` | `from_object_id`, `to_object_id`, `via_meanings`(기본 `connected_to`), `link_types`, `max_steps`, `top_k`, `dead`, `dead_links` | `reachable`(`yes`, `no`, `unknown`), `paths`(`steps`, `length`, `weakest_grade`), `found_count` | `top_k` 기본 3, 상한 10, `max_steps` 상한은 가정. 시간 예산을 넘기면 `truncated` |
| `impact` | `dead`, `dead_links`, `site_failure`, `dead_scope_id`, `mode`(`both`, `facts_only`, `with_assumptions`), `shape` | 5.7절 | 시드 수 상한 1,000은 가정. 결과는 요약을 먼저 반환하고 `items`는 페이지네이션 |
| `exposure` | `dead`(없어도 된다), `scope_id`, `target_object_id`, `cut_order_max` | `tight_groups`, `cut_sets` | 차수 기본 2(기본값(설정)), 상한 3은 가정. `target_object_id`가 있을 때만 `cut_sets`를 계산한다 |
| `changes` | `from_version`/`to_version`(버전 ID) 또는 `from_change_seq`/`to_change_seq`, `object_types`, `source_id`, `scope_id`, `change_kinds`(`added`, `changed`, `retired`) | 타입별·종류별 수, 그다음 `items`(`object_id`나 `link_id`, `change_kind`, `changed_fields`, `change_seq`) | 변경 시퀀스에서 바로 읽는다. 페이지네이션 |
| `versions` | `since_version`(버전 ID), `limit` | 확정 버전들(`kg_version_id`, `change_seq`, `created_at`, `definition_version`, `input_digest`, `change_counts`, `is_head`, `projection_state`: `ready`, `building`, `none`) | 페이지네이션 |
| `quality` | `scope_id`, `source_id`, `metric`, `kg_version_id` | 지표와 목표. 자세한 필드는 ADR-0013이 정한다([데이터 관측성](data-observability.md)) | 지표 수가 적다 |
| `health` | `resource`(소스, 데이터셋, 피드, 타입, 속성, 링크 타입, 파이프라인), `scope_id` | 대상의 헬스 상태와 이유. 자세한 필드는 ADR-0013이 정한다 | 낮은 비용 |
| `scopes` | `scope_id`(없으면 목록) | 저장해 둔 범위(타입, 포함 루트, 필터링), 만든 그룹, 이 버전에서의 `count` | 페이지네이션. 읽기만 한다. 만드는 방법은 10절 |
| `competency` | `question_ids`, `object_ids`, `filter`, `group_by`, `mode`, `breakdown`, `history`, `include` | 충족률, 미충족 객체, 공백(gap). 자세한 필드는 ADR-0015([역량 질문](competency-questions.md)) | 객체별 표시를 집계해 응답한다. `scope_id` 대신 `filter` |

`impact`와 `exposure`와 `paths`는 비용이 큰 질의예요. 질의마다 **비용 등급**(낮음: `get`, `find`, `versions`, `quality`, `health`, `scopes` / 중간: `neighbors`, `containment`, `changes`, `paths`, `competency` / 높음: `impact`, `exposure`)과 시간 예산을 선언하고, 넘기면 잘라서 `truncated`로 알려요. 숫자는 측정해서 정해요. 수치를 반환하는 모든 필드(`counts_*`, `total`, `groups_touched`, `link_counts`, `subtree_size`)는 **숨김 객체를 뺀 수**예요(11절).

### 첫 3종의 기준 구현이 정한 해석 (ADR-0026)

`find`, `get`, `neighbors`는 `core.query`에 기준 구현이 있고, 위 표가 비워 둔 곳은 이렇게 해석해요.

- **읽기 워터마크**: 우선순위는 커서 > `kg_version_id` > `change_seq`(추가 지정) > 최신 순이고, 버전과 변경 번호를 함께 주면 거절해요. 커서는 처음 읽은 변경 번호로 이어서 읽어요.
- **`shape`**: `summary`는 수와 그룹만, `items`는 항목도 반환해요. **조회 기준**: `as_designed`는 `configured`와 `declared` 값만 봐요. `max_staleness`보다 오래된 속성은 `stale`이고 값이 없어요.
- **`get`의 `include`**: 생략하면 `attributes`예요. 속성에는 항상 `grade`, `basis`, `seen_at`, `stale`이 포함돼요. `evidence`는 등급별 개수와 가장 오래된 속성(`worst_freshness`)을 추가해요.
- **`neighbors`**: 순회는 전체 그래프에서 하고 숨김 객체 너머로도 이어 가며, `max_steps`번째 홉에서 만난 숨김 객체는 `hidden_reached`, 앞선 홉에서 지나친 숨김 객체는 `hidden_passed`로 세요. 항목은 (홉, 링크 ID) 순서예요.
- **거절**: 숨김 속성, `volatile` 속성, `scope_id`를 조건으로 쓰면 거절해요(아직 지원하지 않아요).
- **경로와 기본 `shape`**(ADR-0027): 3종은 `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`로 제공하고, 목록용 짧은 경로(`GET /objects`, `/objects/{object_id}/links`)는 같은 질의를 호출해요. 질의는 기본이 `summary`이고 **목록 경로는 기본이 `items`**예요(목록을 호출하는 사람은 항목을 보려고 호출해요). POST 응답에는 ETag를 붙이지 않아요. 읽기 권한 필터링은 호출자의 역할에서 결정돼요. 읽기 역할은 모든 타입, 민감 등급 `open`까지, 전체 범위의 권한 부여 하나를 가진 것으로 간주하고, 저장된 권한 부여가 있으면 그것으로 대체해요.
- **측정 결과**는 [KG 읽기의 측정](kg-read-measurements.md)에 있어요. `get`과 일반적인 `neighbors`는 KG 크기의 영향을 받지 않고(0.1ms 안팎), `find`는 객체 수에 비례해요(10만 객체에서 0.3~0.5초).

## 4. 모든 응답에 붙는 엔벨로프

| 필드 | 설명 |
|---|---|
| `as_of` | 어느 변경 번호까지 반영했나: `kg_id`, KG의 `generation`(시점 복구를 할 때마다 오르는 번호), `change_seq`, 확정 버전 `sealed_version`(있으면). 고정돼 있으면 `pinned`(고정한 버전과 이유) |
| `volatile_as_of` | `volatile` 값을 어느 시점의 것으로 반환했나: `current`(현재 값) 또는 `omitted`("그 버전 시점" 조회라 반환하지 않았다) |
| `served_by` | 어느 역할이 응답했나(`role`: `api_sql`, `engine`), 엔진이면 프로젝션의 `built_at` |
| `freshness` | 그 응답에 쓰인 소스 중 **가장 나쁜 값**: `worst_source_id`, `oldest_age_seconds`, `state`(`fresh`, `stale`, `no_expected_period`). `stale`은 기대 주기의 두 배를 넘긴 것(ADR-0011, 가정) |
| `evidence` | `min_grade`(쓰인 입력 중 가장 낮은 등급), `counts`(`fact`, `assumed`, `unknown`), `kinds`(쓰인 근거의 종류: `configured`, `observed`, `declared`, `derived`), `observation_age_seconds` |
| `result_complete` | `total`, `returned`, `truncated`, `reason`(`limit`, `time_budget`, `size_budget`), `next_cursor` |
| `hidden` | `hidden_passed_count`(응답이 경유한 숨김 객체의 수)와 `hidden_reached_count`(응답이 도달한 끝에 있는 숨김 객체의 수). 범위가 전체가 아닌 주체에게는 정확한 수 대신 있음/없음만 제공해요(설정으로 정확한 수를 공개할 수 있어요). 내용은 없다 |
| `health_hint` | 이 응답에 쓰인 것 가운데 헬스 상태가 비정상인 것의 개수와 목록(ADR-0013) |
| `model_limits` | 이 응답이 다루지 못하는 한계의 문구(14절) |
| `warnings` | 예: `capacity_group_unknown`(용량 기반 그룹을 아직 계산하지 않아 알 수 없음으로 반환했다) |
| `data` | 질의별 본문 |

**고정(pin).** 잘못된 데이터가 들어왔을 때 `operator`(전체 범위)나 `admin`이 "질의는 버전 V로 응답하라"고 고정할 수 있어요(`serving_pin`, ADR-0014). 데이터를 되감지 않고 읽기 측만 고정하고, 응답의 `as_of.pinned`에 표시돼요. 고정하면 질의 엔진이 그 버전을 로드해요. 로드가 끝날 때까지 고정은 `pending`이고, 그동안은 현재 버전으로 응답하며 `as_of.pinned`에 `pending`이라고 표시해요. 로드가 끝나면 고정이 적용돼요(`active`).

**`volatile` 값의 처리 경로.** `volatile` 속성이 바뀌면 이력 없는 **경량 변경 레코드**(객체, 속성, 새 값의 표시, `change_seq`)가 기록돼요. 값이 같아 기록하지 않은 변경은 레코드도 없어요(ADR-0009). 프로젝션, 충족 표시, 변경 알림은 적용된 변경 로그와 함께 이 레코드를 따라가요. 이 레코드는 `hist`와 버전의 해시에 포함되지 않고 짧게 보존돼요([기본값 표](defaults.md)). 그래서 `as_observed`가 읽는 관측 상태가 `volatile`이어도 엔진이 알 수 있어요. **"그 버전 시점" 조회는 `volatile` 값을 반환하지 않아요**(현재 값을 섞지 않아요). `volatile_as_of`가 `omitted`로 알려요. **링크는 `volatile`이 될 수 없어요.** 영향 범위가 읽는 링크는 자주 이동하는 `runs_on`도 일반 변경이에요(쓰기량은 측정해서 확인해요).

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

## 5. 영향 범위의 의미

### 5.1 상태는 채널마다, 링크 의미는 채널로

한 객체의 상태는 "정상/장애" 하나가 아니에요. 채널(`service`, `power`, `cooling`, `network`, `storage`, `management`)마다 `up`, `down`, `unknown`이에요. "서버는 켜져 있는데 네트워크에서 끊겼다"와 "서비스는 실행 중인데 관리 경로가 끊겼다"를 구분하려는 것이에요. 링크 의미 11가지는 "출발 객체의 어느 채널이 도착 객체의 어느 채널에 의존하나"로 정의돼요. 정의는 ADR-0007이 소유하고, 여기서는 계산에 쓰는 형태만 적어요.

| 링크 의미 | 계산 방식 |
|---|---|
| `runs_on` | 출발의 `service` ← 도착의 `service`. 링크 속성 `failover_to`(그룹)가 있으면 5.4절 |
| `requires`(`hard`/`soft`) | `service` ← `service`. soft는 장애가 되지 않고 `degraded` |
| `supplied_by`(`power`/`cooling`/`network`/`storage`) | 출발의 그 채널 ← 도착의 `service` |
| `realized_by` | `service` ← `service` |
| `part_of` | 전체의 `service`에 장애가 나면 부품도 장애가 된다. 전체가 부품을 필요로 하는지는 `whole_needs_part`(`hard`/`soft`/`none`, 기본 `none`)나 이중화 그룹으로 정한다 |
| `contained_in` | 물리적 포함 관계다. 사이트 단위의 장애(`site_failure`)에서만 안에 든 객체로 전파된다 |
| `connected_to` | 연결성 계산의 간선. 양 끝이 정상일 때만 쓴다. `network` 경로를 계산할 때 쓴다 |
| `member_of` | 그룹의 구성원(링크 속성 가중치, 역할) |
| `managed_by` | 출발의 `management` ← 도착의 `service` |
| `owned_by`, `record_only` | 전파되지 않는다. 프로젝션에도 넣지 않는다 |

타입은 자기 `service`가 어느 채널을 필요로 하는지를 특성으로 선언해요(`Powered`는 `power`, `Cooled`는 `cooling`, `NetworkAttached`는 `network`, 스토리지를 쓰는 타입은 `storage`를 hard로. 타입이 강도를 바꿀 수 있어요). 링크에는 `delay_class`(`immediate`, `delayed`)가 있어요. 링크는 모두 일반 변경이라 엔진에 도달해요(`volatile`이 될 수 없어요).

### 5.2 이중화 그룹

`RedundancyGroup`(하위 타입 `Lag`, `HaCluster` 같은 것)은 방식이 2가지예요.

| 방식 | 계산 방법 |
|---|---|
| `count` | 정상 구성원이 `min_alive`개 이상이어야 한다 |
| `capacity` | 정상 구성원의 용량 합이 의존하는 쪽의 수요 합 이상이어야 한다. 수요는 의존 링크의 속성 `demand`에 기록한다 |

모델은 두 방식을 모두 갖고, **엔진은 `count`를 먼저 구현해요.** 그 전까지 `capacity` 그룹의 결과는 `unknown`이고 `warnings`로 알려요. `count`는 구성원 중 정상인 수 `a`, 알 수 없는 수 `u`로 계산해요.

| 조건 | 그룹의 값 |
|---|---|
| `a >= min_alive` | 정상이다. `slack = a - min_alive`가 여유다 |
| `a + u < min_alive` | 장애다 |
| 그 밖 | 알 수 없음 |

알 수 없는 구성원이 있어도 "안전하다"로 읽히지 않아요. 구성원이 정상인지는 **그 구성원의 의존을 끝까지 따라가서** 정해요. 겉으로는 이중화인데 같은 PDU에 연결돼 있으면 한꺼번에 장애가 나요([예시 문서](kg-queries-examples.md) 1절). ECMP, LACP, BGP 그룹은 질의할 때 계산하지 않고 **수집할 때 만들어 저장해요**. 같은 그룹을 설정(`configured`)에서 도출할 수도, 라우팅 테이블이나 이웃 테이블(`observed`)에서 수집할 수도, 설계 문서(`declared`)에서 수집할 수도 있어요. 경로가 여럿이면 모두 수집해 두고, 설정과 관측이 일치하지 않으면 문제 목록에 올라요. 질의는 `view`로 어느 쪽을 쓸지 선택해요. 설정을 온전히 읽은 그룹은 `fact`예요. `min_alive`에도 근거 등급이 있고, 알 수 없으면 1부터 `n`까지 가능한 모든 값에서 결과가 같을 때만 그 결과로 확정해요(예시 문서 5절).

### 5.3 의존과 연결성, 교대 계산으로 최소 고정점까지

`network` 채널은 2가지로 계산해요.

| | 의존 | 연결성 |
|---|---|---|
| 질문 | 장애가 그룹의 임계값을 넘어 전파되나 | 앵커까지 경로가 남아 있나 |
| 대상 | 설정에서 도출한 그룹(LACP, ECMP, 쌍을 이룬 스위치)과 5.1의 의존 | `connected_to` 간선 위의 경로. 앵커는 특성 `NetworkAnchor`를 단 객체 |
| 방법 | 장애 객체에서 출발해 그룹마다 정상 구성원 수를 센다 | 앵커에서 정상 간선을 따라 도달하는 객체를 찾는다 |
| 오류 양상 | 순서가 있는 의존이면 정확하다 | 루프가 있는 네트워크에서 개수만 세면 낙관적으로 틀린다 |

둘로 나누는 이유는 **조사 담당의 추론**이에요. 문헌으로 확인한 사실이 아니에요. 루프가 있는 네트워크에서 두 스위치가 서로를 정상 이웃으로 세면, 둘 다 상위와 끊겨도 서로 때문에 정상으로 읽는다는 거예요. 그래서 말로만 믿지 않고 **기준 구현과 대조하는 테스트**로 검증해요(7절).

계산은 **최소 고정점**이에요. 처음에는 모두 정상이고, 장애로 지정한 객체에서 출발해 규칙이 강제하는 것만 장애가 돼요. 의존 계산에서 장애가 된 스위치는 연결성 계산의 간선에서 빠지고, 도달하지 못하는 객체는 다시 의존 계산의 장애 객체에 들어가요. **두 계산을 교대로 반복해 더 늘지 않을 때 멈춰요.** 장애 객체는 늘기만 하므로 반드시 멈춰요.

연결성도 3가지 값이에요. 정상 간선만으로 도달하면 `yes`, 정상 간선과 알 수 없는 간선을 모두 써도 도달하지 못하면 `no`, 그 밖은 `unknown`이에요. **범위 안에 앵커가 없으면 `unknown`이에요.** "안전"이 아니에요.

### 5.4 객체별 결과
| 결과 | 설명 |
|---|---|
| `down` | (심각도가 가장 높음) `service`에 장애가 난다. `lost_channels`가 이유를 나타낸다. 네트워크만 잃었으면 "연결 끊김"으로 읽힌다 |
| `interrupted` | 장애가 나지만 선언된 페일오버(`runs_on`의 `failover_to` 그룹)가 정상이라 복구될 것으로 본다. 그룹의 여유를 모르면 `unknown` |
| `degraded` | 정상이지만 soft로 의존하던 것을 잃었거나, 의존하는 그룹의 구성원이 줄었다(여유는 남았다) |
| `exposed_direct` | 정상이지만 자신이 의존하는 그룹의 여유가 0이다(이중화를 잃었다). 하나만 더 장애가 나면 장애가 된다 |
| `exposed_inherited` | 정상이지만 의존 대상이 노출됨이다(이중화를 잃은 랙 위의 VM) |
| `unmanageable` | `service`는 정상이고 `management`만 잃었다 |
| `unknown` | 알 수 없는 입력 때문에 정할 수 없다 |

한 객체에 결과가 여러 개 해당하면 가장 심각한 것 하나를 `result`로 반환하고 나머지는 `also`에 적어요. 심각도 순서는 `down` > `interrupted` > `unknown` > `exposed_direct` > `exposed_inherited` > `degraded` > `unmanageable`이에요.

**노출됨은 끝까지 전파돼요. 전파 깊이를 정하는 설정은 없어요.** 같은 KG의 같은 질의가 배포마다 다른 응답을 반환하면 안 되고, 기준 구현과 골든 값을 깊이마다 따로 두면 안 돼서예요. 응답은 직접 노출과 상속된 노출을 구분하고 `counts_by_result`도 구분해요. 전파된 만큼 질의가 도달하는 노드가 늘어요(12절).

시간은 계산하지 않아요(냉방을 잃고 버티는 시간, 배터리 지속 시간, 페일오버에 걸리는 시간). 링크의 `delay_class`가 `delayed`면 응답에 "지연 발생"(`delay_class`)으로 표시만 해요. 지속 시간 같은 값은 속성으로 저장해 둬요.

### 5.5 여러 객체가 함께 장애 나는 질의, `exposure`, 최소 절단
`dead`는 객체 ID 목록이에요. 시드를 합쳐서 한 번에 계산해요. 장애는 늘기만 하므로(단조) 시드의 순서에 따라 결과가 달라지지 않아요. `dead_links`는 장애 링크, `site_failure`는 사이트 단위의 장애(`contained_in`으로 안에 든 객체가 장애가 된다), `dead_scope_id`는 저장해 둔 범위 전체예요.

`exposure`는 `dead`가 주어진 상태에서 **정상이고 여유(`slack`)가 0인 그룹**을 골라요. 그 그룹의 정상 구성원이 후보예요. 후보마다 "이것까지 장애가 나면" 새로 `down`이 되는 객체 수(`would_die_count`)를 산출해요. 후보는 모든 객체가 아니라 여유가 없는 그룹의 구성원과 그 의존의 상위 객체라 작아요. `dead`가 비어 있으면 지금 이미 여유가 0인 그룹의 목록이에요. 그룹이 아닌 단일 의존은 모든 객체에 있으므로 대상이 아니에요.

`target_object_id`를 주면 **최소 절단**("어떤 객체 2~3개가 함께 장애 나면 끊기나")을 계산해요. 일반적으로 어려운 문제라(하위식을 공유하면 NP-hard라고 조사 담당이 일반 지식으로 적었고 문헌은 열람하지 못했어요) **낮은 차수까지만** 계산해요. 기본 2, 상한 3은 가정이에요. 후보는 공통 조상을 공유하는 쌍으로 좁혀요. 응답의 `cut_complete_up_to`가 어느 차수까지 모두 계산했는지 적어서, 계산하지 않은 차수가 "없다"로 읽히지 않게 해요.

### 5.6 사실만 쓴 응답, 가정 포함 응답, 알 수 없음

| 응답 | 입력 해석 방식 |
|---|---|
| 사실만 쓴 응답(`facts_only`) | `fact` 입력만 쓴다. `assumed` 입력은 **알 수 없음으로 읽는다**(없는 것으로 보지 않는다). `min_alive`를 알 수 없으면 가능한 모든 값에서 같을 때만 그 결과 |
| 가정 포함 응답(`with_assumptions`) | `assumed` 입력도 `fact`처럼 쓴다 |
| 알 수 없음(`unknown`) | 두 응답 어디서든 `unknown` 입력 때문에 정할 수 없는 객체. 따로 반환한다 |

사실만 쓴 응답에서 확정된 결과는 가정 포함 응답에서도 같아요. 알 수 없음은 가정을 넣으면 확정될 수 있어요. 7절이 불변식으로 검증해요. 순환 의존(자기가 관리하는 클러스터 위에서 실행되는 컨트롤 플레인 같은 것)은 최소 고정점이라 **스스로 장애가 되지 않아요.**

### 5.7 `impact`의 응답

| 필드 | 설명 |
|---|---|
| `facts_only`, `with_assumptions` | 각각 결과별 목록(`down`, `interrupted`, `degraded`, `exposed_direct`, `exposed_inherited`, `unmanageable`, `unknown`). 먼저 `counts_by_result`, `counts_by_type`, `counts_by_parent`(상위 위치), 그다음 `items` |
| `items` | `object_id`, `object_type`, `result`(가장 심각한 것), `also`(나머지), `lost_channels`, `delay_class`, `via`(직전 원인 하나), `data_health`(쓰인 데이터가 이상 상태면 표시) |
| `groups_touched` | 영향받은 그룹(`group_id`, `mode`, `value`, `alive`, `unknown`, `n`, `min_alive`, `slack`). 읽을 수 없는 그룹은 나오지 않고 `hidden`으로만 집계돼요 |
| `reachability` | 범위 안의 앵커 수, 도달하지 못하게 된 객체 요약. 앵커가 없으면 `network`는 `unknown` |

`exposed_direct`, `exposed_inherited`, `degraded`는 **시드 때문에 달라진 객체**에만 나와요. 원래 여유가 0인 그룹은 `exposure`가 담당해요. `max_staleness`를 넘은 관측은 **쓰지 않고** 그 입력을 알 수 없음으로 읽어요. 설계 기준(`as_designed`)에 입력이 있으면 그것으로 폴백해요. 응답의 `stale_inputs`와 `warnings`(`stale_observation_not_used`)가 그렇게 했다고 알려요. 영향 범위가 이상 상태인 데이터(소스 중단 등)에 의존하면 `data_health`로 알려요(ADR-0013).

## 6. 손으로 따라가기

전원, LAG, HA 클러스터, MLAG, N+1 냉방의 가상의 예시 5개는 [영향 범위를 손으로 따라가기](kg-queries-examples.md)에 있어요. 사실만 쓴 응답과 가정 포함 응답이 달라지는 지점, 의존만 쓰면 틀리는 지점, 직접 노출됨과 상속된 노출됨이 나뉘는 지점을 보여 줘요.

## 7. 기준 구현과 속성 기반 테스트

**기준 구현**은 느리지만 단순하고 명확한 계산이에요. 모든 객체를 "정상"으로 놓고, 시드를 장애로 바꾼 뒤, 정의대로 모든 객체의 채널 값을 다시 구하는 과정을 더 바뀌지 않을 때까지 반복해요(최소 고정점, 3가지 값). 연결성은 앵커에서 시작하는 너비 우선 탐색이에요. 엔진은 빨라야 하므로 다르게 구현하고, **엔진의 결과 = 기준 구현의 결과**여야 해요. 기준 구현은 `core.query`의 일부이고 엔진 포트의 계약 테스트와 함께 제공돼요. 엔진을 새로 작성하거나 크게 수정할 때도 같은 골든 파일과 같은 테스트를 통과해야 해요. **기준 구현에는 설계의 의미가 모두 들어가요**: 3가지 값, 2가지 응답, 의존과 연결성의 교대 계산, 6개 채널의 동시 상태, 직접·상속된 노출됨, 근거와 관측 시각과 `max_staleness`, `volatile` 레코드를 반영하는 관측. 영향 범위 단계의 첫 작업이 이 기준 구현이고, 합성 세계 생성기의 정답 데이터를 이것으로 만들어요([엔진의 모양](query-engine-shape.md)).

| 불변식 | 설명 |
|---|---|
| 엔진 = 기준 구현 | 임의로 생성한 회로(의존, 그룹, 연결성)와 루프가 있는 네트워크, 합성 세계에서 결과가 같다 |
| 단조 | 시드를 추가하면 `down`인 객체가 줄지 않는다. `unknown`과 `exposed_direct`·`exposed_inherited`는 단조가 아니다 |
| 장애 객체 추가 | 이미 장애인 객체를 시드에 추가해도 결과가 같다. 시드의 순서와 객체 번호의 순열에 따라 결과가 달라지지 않는다 |
| 스스로 장애가 되지 않음 | 시드가 없으면 아무것도 장애가 되지 않는다. 순환 의존 입력도 같다 |
| `view` | `as_designed`의 결과는 관측 입력이 바뀌어도 변하지 않는다 |
| `min_alive` | 낮추면 `down`인 객체가 늘지 않는다. 1이면 "하나라도", `n`이면 "모두"와 같다 |
| 구성원 | 정상 구성원을 추가하면 `down`인 객체가 늘지 않는다 |
| 사실과 가정 | 사실만 쓴 응답에서 확정된 결과는 가정 포함 응답에서도 같다 |
| 루프 | 루프가 있는 무방향 네트워크에서 연결성이 너비 우선 탐색과 같다(6.4의 오답 사례를 포함). 앵커가 없으면 `unknown` |
| 프로젝션 | 같은 변경 시퀀스이면 같은 프로젝션(내용 해시가 같다). 변경을 증분 갱신한 프로젝션 = 전체를 다시 빌드한 프로젝션 = 조각별로 병합한 프로젝션 |
| 병합과 교체 | 조각을 병합하거나 버전을 교체하는 중의 응답은 이전 버전의 응답이거나 새 버전의 응답이다. 섞이지 않는다 |
| 노출됨 | 상속된 노출됨은 원인을 거슬러 올라가면 언제나 직접 노출됨에 도달한다. 전파 깊이에 따라 결과가 달라지지 않는다 |
| 응답 필드 | `as_of`가 있고, `truncated`가 참이면 `next_cursor`나 이유가 있다 |

## 8. 프로젝션

메모리 안의 그래프예요. 파생 데이터라 다시 만들 수 있고 고가용성의 대상이 아니에요. 질의 엔진이 로드해 영향 범위, `exposure`, 경로 질의에 응답해요. **워커가 만든 초기 버전**에 **변경 시퀀스**를 수신해 증분 갱신해요.

| 포함하는 것 | 설명 |
|---|---|
| 객체 테이블 | 안정 ID(16바이트)와 내부 번호(32비트)의 대응, 타입 번호(`type_no`), 폐기(retired) 표시. 내부 번호는 인스턴스마다 달라서 응답과 커서에 포함되지 않아요 |
| 이름과 타입의 소형 테이블 | 표시 이름과 타입 이름(스토리지가 멈춰도 응답에 이름이 붙게). **민감한 속성은 넣지 않는다** |
| 링크 의미별 이웃 배열 | 링크 의미(`runs_on`과 `failover_to`, `requires`의 `hard`/`soft`, `supplied_by`의 채널별, `realized_by`, `part_of`와 `whole_needs_part`, `member_of`, `contained_in`, `connected_to`, `managed_by`)마다 정방향과 역방향. 링크마다 근거 등급, 근거의 종류, 관측 시각, `delay_class` |
| 그룹 테이블 | 그룹 ID, 구성원, 방식(`count`, `capacity`), `min_alive`와 그 등급, 가중치, 의존 링크의 `demand` |
| 특성 테이블 | 객체의 특성(`Powered`, `NetworkAnchor` 등)과 필요로 하는 채널의 강도 |
| 읽기 권한용 테이블 | 객체의 타입 번호, 객체별 `scope_keys`(타입에 `scope_key: true`로 선언한 속성. 타입마다 4개까지) 속성의 값, 속성의 민감 등급. API가 응답을 필터링할 때 쓴다(ADR-0012) |
| `volatile` 관측 상태 | `as_observed`가 읽는 관측 상태(기동 중인지 같은 것)를 객체별로 보유하고, 경량 변경 레코드를 따라 갱신해요 |
| 목록 헤더 | 소스별 `last_complete_at`과 기대 주기(엔진이 신선도를 단독으로 계산하려고) |

**파일 형식 요구사항.** 특정 언어의 직렬화에 종속되지 않아요. 평탄한 배열과 헤더(형식 버전, 반영한 `change_seq`, 그래프 해시, 섹션 테이블, 섹션별·전체 내용 해시)로 구성돼요. 해시는 정규 바이너리 인코딩 위의 SHA-256이에요(ADR-0006). 순서는 모두 고정된 순서로 정렬해요. **컨테인먼트 단위의 조각으로 나눠 로드할 수 있어야 해요.** 조각마다 해시가 있고, 조각을 가로지르는 간선은 두 조각의 **경계 테이블**에 둬요. 컬럼 형식은 구현하는 변경에서 정해요. 파일 스토리지에는 워커만 쓰고, 읽기는 엔진 역할만 해요(프로젝션에 이름과 `scope_keys` 값이 들어 있어요). 새로 기동하는 엔진이 로드할 프로젝션이 없으면 스토리지에서 직접 빌드해요(파일 스토리지는 빠른 기동을 위한 것이고 필수는 아니에요).

**증분 갱신 방식.** 베이스 배열(조각 안에서 바뀌지 않는 버전)과 추가 오버레이(추가한 간선은 출발 객체별 목록, 삭제한 간선은 툼스톤 표시)로 구성돼요. 읽기 측은 (베이스 버전, 오버레이의 `change_seq` 위치)를 보유하고, 오버레이는 `change_seq` 순서로 추가만 하므로 읽기에 락이 필요 없어요. 엔진은 프로젝션에 반영할 변경과 경량 변경 레코드를 포트로 수신해요. 오버레이가 쌓이면 질의가 느려져서(1억 규모에서 간선의 1% 남짓이 쌓이자 1홉 질의의 중앙값이 3ms에서 14ms로 올랐어요) 병합(컴팩션) **주기의 예산**을 둬요.

**전체를 한 번에 병합하지 않아요.** 병합하는 순간의 메모리가 평소의 약 2.4배라서, 베이스를 조각으로 나눠 **조각별로 병합하고 버전 교체도 조각 단위로** 해요. 엔진의 메모리 예산은 "평소 + 가장 큰 조각의 병합"이에요([엔진의 모양](query-engine-shape.md)). 전체를 다시 빌드해 교체하는 경우는 초기 적재와 차이가 임계값을 넘는 정의 병합뿐이에요. 일반 병합 결과는 일반 변경 배치로 흘러가요. 교체할 때 프로젝션 파일은 워커가 만들어 **파일 스토리지 포트**에 업로드해요(어댑터: 파일 시스템이나 객체 스토리지. PostgreSQL에 넣지 않는다). 엔진은 파일을 내려받아 자기 디스크에 두고 해시를 확인한 뒤 로드하고, 새 질의를 새 버전으로 전환해요. 진행 중이던 질의가 끝나면 이전 버전을 해제해요. 인스턴스를 하나씩 순서대로 교체해요.

## 9. 질의 엔진이라는 역할

ADR-0006의 역할 4종(API, 워커, 질의 엔진, 수집기) 중 하나예요. 같은 실행 파일에서 진입점만 달라요.

- **여러 인스턴스.** 부하에 따라 인스턴스 수를 늘려요. 각 인스턴스가 자기 메모리에 KG별 프로젝션을 보유해요.
- **스토리지가 멈춰도 응답해요.** 로드해 둔 프로젝션으로 응답을 이어 가고, 엔벨로프에 `as_of`와 신선도(목록 헤더의 시각으로 계산)가 기록돼요. 새 변경은 수신하지 못하니 이전 시점의 응답이라는 것이 드러나요.
- **호출자는 엔진을 직접 호출하지 않고 언제나 API를 거쳐요.** API는 스토리지가 멈춰도 질의 경로에 응답해요. 로그인 토큰은 스토리지 없이 검증하고, 그룹과 역할의 매핑은 경과 시간에 상한이 있는 캐시를 써요(ADR-0011, ADR-0012).
- **엔진의 경계**는 프로젝션 파일과 변경 시퀀스예요. 엔진은 그 둘(경량 변경 레코드 포함)만 읽고 정해진 계약으로 응답해요. 엔진은 Go로 직접 구현해요(확정. ADR-0006, [엔진의 모양](query-engine-shape.md)). 이 경계는 엔진을 독립적으로 테스트하고 수정하려는 것이고 언어를 바꾸려는 것이 아니에요.
- **API와 엔진 사이의 계약.**

| 방향 | 필드 |
|---|---|
| API → 엔진 | KG의 ID와 세대, 질의 이름, 인자(`view`, `max_staleness` 포함), `kg_version_id`(없으면 현재), 시간·크기 예산, 요청 ID |
| 엔진 → API | 엔벨로프와 본문(객체 ID, 타입, 등급을 포함해서 API가 읽기 권한으로 필터링할 수 있게), 또는 오류(`version_not_loaded`+보유 중인 범위, `budget_exceeded`+범위를 좁히는 방법, `too_many_seeds`) |
| 엔진 → 조율 | 반영한 `change_seq`, 준비 상태, 프로젝션의 `built_at` |

- **엔진이 로드해 두는 버전.** 현재 버전과 고정된(`serving_pin`) 버전만이에요. 그 밖의 과거 버전의 `impact`는 요청하면 따로 로드해 응답하고, 로드하는 동안은 `version_not_loaded`(보유 중인 범위 포함)로 응답해요. 고정을 적용한 버전은 따로 요청하지 않아도 엔진이 로드해요. `find`, `get`, `changes` 같은 SQL 질의는 과거 버전도 바로 읽어요.
- **SQL로 처리할 질의와 엔진으로 처리할 질의.** 기본 분류는 이래요. `find`, `get`, `changes`, `versions`, `quality`, `health`, `scopes`, `containment`, `competency`(표시를 집계하는 것)는 SQL, `neighbors`는 1~3홉까지 SQL, `paths`, `impact`, `exposure`는 엔진이에요. SQL로 어디까지 처리할 수 있는지는 합성 규모에서 SQL(재귀)로도 구현해 측정해서 가려요. SQL이 목표를 충족하면 엔진의 범위가 줄 수 있어요. 개발 환경에서는 한 프로세스에 역할을 함께 기동해도 포트를 거쳐요.

## 10. 인터페이스 3가지

### HTTP API

- **KG는 계약상의 자원이에요.** KG에 든 것(객체, 링크, 질의, 변경, 버전, 충족률, 헬스 상태, 재수집)은 `/v1/kgs/{kg}/...`이고, 정의와 권한 부여와 시크릿과 운영은 배포마다 하나라 `/v1/...` 바로 아래예요. 정의의 메인 브랜치는 배포마다 하나이고 KG는 그 메인 브랜치의 어느 정의 세트를 쓰는지 지정해요. 권한 부여의 범위에 KG를 고르는 필드가 있어요(기본 `main`).
- **버전이 있는 계약.** 호환성을 깨는 변경은 새 버전으로 발행해요. 계약은 `core.contract`가 소유하고, 경로 규칙과 목록은 [REST API 설계](rest-api.md)(ADR-0016)가 정해요.
- **서비스 계정.** 사람이 아닌 시스템이 쓰는 계정도 같은 역할(ADR-0012)을 부여받아요.
- **호출자별 한도.** 초당 요청 수, 동시 요청 수, 계산 예산(비용 등급별)을 계정별로 정해요. 기본값은 측정해서 정해요. 한도를 넘으면 오류가 대기 시간이나 범위를 좁히는 방법을 알려요.
- **대량 읽기(`bulk_read`).** 범위 하나(`scope_id`)를 한 버전에서 통째로 읽는 경로예요. 개별 질의를 수만 번 호출하게 두지 않으려는 것이에요. 페이지 단위 스트림으로 내려받고, 같은 엔벨로프가 붙고, 숨김 처리가 적용되고, 사용한 주체가 감사 로그에 남아요(ADR-0012). 사전 정의 질의 13종에 속하지 않는 별도 경로예요.
- **사용 이력.** 누가 어느 타입과 속성을 읽었는지를 서비스 계정별로 API가 기록해요(일반 읽기는 감사 로그가 아니라 사용 통계예요. 민감한 속성과 대량 읽기는 감사 로그에 남아요. ADR-0012, ADR-0013). 정의의 계보가 "누가 쓰나"에 답하는 데 이 이력을 써요.
- **쓰기 경로 2가지.** 저장해 둔 범위는 정의가 아니에요. 읽기 역할이 API로 만들고, 만든 그룹에 속해요. KG가 틀렸다고 **신고하는 경로**도 API에 있고 신고는 문제 목록에 들어가요(ADR-0011). 둘 다 KG를 변경하지 않아요.

### 읽기 전용 MCP

- 툴 목록은 사전 정의 질의 13종과 같은 이름이에요(`find` ... `scopes`, `health`, `competency`). 여기에 **재수집**(지정 재수집)과 **기대와 대조**를 읽기 인터페이스로 추가해요(ingestion-extensions.md). `bulk_read`는 에이전트에게 제공하지 않아요. **KG의 MCP는 읽기만** 해요(운영 MCP 인터페이스에는 액션이 있어요. self-observability.md). 현실의 시스템과 정의를 변경하지 못하고, 범위를 만들거나 문제를 신고하는 경로도 없어요. 재수집은 소스에서 읽는 작업이지만 같은 수집 경로로 적용되므로 작업(operation)을 생성해요. 이 작업을 생성할 수 있는 역할은 15절의 미결 사항이에요. 툴 설명에 이 의미를 명시해요.
- 응답 크기에 **상한**이 있고 넘으면 잘라서 질의 범위를 더 좁히는 방법을 안내해요. 상한의 숫자는 가정이고 측정해서 정해요.
- **간략 보기와 상세 보기**(`detail`: `brief`, `full`). 기본은 간략 보기예요. 사람이 읽는 이름과 짧은 ID를 함께 제공해요.
- **오류가 다음에 할 일을 안내해요.** 예:

```yaml
error: {code: ambiguous_name, message: "이름이 같은 객체가 3개예요",
        next_step: "object_type을 더해서 다시 물어요",
        candidates: [{object_id: "obj-31", object_type: "PhysicalServer", display_name: "srv-31"}]}
```

### 변경 알림

- **알리는 대상.** 변경으로 바뀐 것: 객체와 링크의 추가, 변경, 삭제(retired), 그리고 그룹(`min_alive`)의 변화. 변경 시퀀스에서 생성되고, `volatile` 속성의 변경은 경량 변경 레코드를 따라 이력 없는 알림으로 발행돼요. 데이터 헬스 상태 이벤트도 이 경로로 발행할 수 있어요(ADR-0013).
- **풀(pull) 방식을 먼저 제공해요.** 변경 목록은 `changes`와 같은 형식이에요. 수신 측이 마지막으로 가져간 `change_seq`를 `from_change_seq`로 전달하면 그다음부터 반환해요. 읽기 권한은 목록에도 적용돼요(타입별 수도 숨김 객체를 뺀 수예요).
- **푸시 방식**은 나중에 제공해요. 전송 경로는 포트 뒤에 두고, 수신 측별 필터링과 전달 보장은 그때 정해요.

## 11. 읽기 권한과 숨김 객체

권한의 규칙은 [읽기 권한과 역할](access-control.md)과 ADR-0012가 정해요. 읽기는 권한 부여의 범위(객체 타입, 민감 등급의 한도, 클로즈드 베타 전에는 객체의 조건)예요. 조건에 맞지 않는 객체는 숨김 객체와 같이 다뤄요. 여기서는 영향 범위와 관련된 것만 적어요.

- **계산은 전체 그래프에서 해요.** 호출자에 따라 계산이 달라지면 결과가 틀려요. **엔진의 응답을 API가 필터링하는 것**이 읽기 권한이에요(프로젝션의 읽기 권한용 테이블을 써서요).
- 영향 범위가 읽을 수 없는 객체를 경유하면 `hidden.hidden_passed_count`에, 영향받는 끝에 도달하면 `hidden.hidden_reached_count`에 집계돼요. 범위가 전체가 아닌 주체에게는 정확한 수 대신 있음/없음만 제공해요(설정으로 정확한 수를 공개할 수 있어요). 그 객체는 결과 목록에 나오지 않고 내용도 타입도 제공하지 않아요. `via`는 "숨김 객체"로 표시돼요.
- **필터링 뒤의 페이지는 부족한 만큼 채워요.** API가 엔진이 준 페이지를 필터링하고 남은 것이 `limit`보다 적으면 엔진에 추가로 조회해 `limit`까지 채워 반환해요(`result_complete.total`은 필터링 뒤의 값).
- **수를 반환하는 모든 필드는 숨김 객체를 뺀 수예요.** `counts_*`, `total`, `groups_touched`, `would_die_count`, `subtree_size`, `link_counts`, `neighbors`의 `groups`, 충족률의 분모, 변경 알림의 타입별 수가 해당돼요. 같은 타입 안에서 읽기 권한이 없는 객체의 수가 곧 정보일 수 있어서예요. 계약에 그렇게 적어요.
- `paths`가 숨김 객체를 경유하면 그 홉은 "숨김 객체 하나"로 보여요(홉 수는 드러나요).
- **반복 질의로 알아내는 추론은 완전히 차단하지 못해요.** `impact`와 `paths`는 장애 대상(`dead`, `dead_links`)을 호출자가 골라요. 보이는 객체를 하나씩 장애로 지정해 가며 `hidden`의 있음/없음이 바뀌는 것을 보면 숨김 객체 쪽의 의존을 추정할 수 있어요. 정확한 수를 주지 않는 것이 첫 번째 방어선이에요.
- 숨김 속성은 "숨김"으로 표시되고, 검색 조건으로 쓰면 거절해요. 읽을 수 없는 객체의 ID를 `dead`에 넣으면 "찾을 수 없음"과 같은 형태로 응답해요.

## 12. 성능

요구사항은 **제품 책임자가 정한 숫자**예요. 규모 요구는 한 KG에 수천만 개, 설계 여유는 수억까지예요(REQ-134).

| 질의 | 요구사항 | 근거 상태 |
|---|---|---|
| 스위치의 ECMP·BGP·LACP를 다 고려한 영향 범위 | 1초 안에 | **아직 근거가 없는 목표예요.** 연결성 계산을 포함한 형태로 측정하지 않았어요 |
| 설비 문제가 VM·앱·서비스에 미치는 범위 | 수 초 안에 | 합성 그래프를 쓴 프로토타입에서 자릿수로는 이 범위 안이에요(가정) |

인계받은 "3초"는 쓰지 않아요. ECMP·BGP·LACP는 라우팅 테이블이 아니라 장비 설정 값에서 수집할 때 만든 그룹으로 계산해요.

**측정 결과.** 같은 엔진의 프로토타입을 합성 그래프(노드와 간선 1억 개까지, 2코어 8GB)에서 측정했어요. 그 이상은 추정이에요. 1홉 질의의 중앙값은 약 2ms이고, 응답 시간은 KG의 크기가 아니라 질의가 도달하는 노드 수에 비례해요. **이 수치에 포함되지 않은 것**(3가지 값, 2가지 응답, 연결성의 교대 계산, 링크별 근거와 관측 시각, 속성 필터링, 과거 버전, 읽기·쓰기 동시 처리, 입출력 등)과 메모리가 12바이트의 몇 배가 된다는 것은 [엔진의 모양](query-engine-shape.md) 4절에 있어요. 그래서 위 숫자를 충족했다고 말하지 않아요.

**남은 측정.** 영향 범위 단계의 첫 작업은 설계의 의미를 모두 반영한 기준 구현이고(7절), 그 위에서 다시 측정해요.

| 순서 | 측정 항목 |
|---|---|
| 1 | SQL만으로 응답하는 질의의 기준선(재귀로 구현한 영향 범위 포함). 프로젝션이 필요한 범위를 숫자로 정한다 |
| 2 | 스토리지에서 프로젝션 빌드: 시간과 메모리 |
| 3 | 영향 범위의 응답 p50/p95/p99. 허브, 깊은 체인, 루프, 큰 영향 범위, 그룹이 많은 입력, 사이트 간 교차 의존, 동시 호출을 포함한다 |
| 4 | 조각 병합: 가장 큰 조각의 병합 메모리, 진행 중 질의에 주는 영향, 오버레이가 쌓일 때의 지연(병합 주기의 예산) |
| 5 | 응답 크기(대표 MCP 호출이 상한 안에서 요약되나)와 결정성(순서를 섞고 워커 수를 바꿔도 같은 프로젝션, 같은 응답) |

스토리지 측(순서 1, 2)의 수천만 건 측정은 2코어 8GB로는 부족해서 클로즈드 베타 전에 여유 장비 하나가 필요해요. 안 되면 먼저 파라미터, 인덱스, 파티셔닝을 조정해 보고, 그래도 안 되면 제품 책임자에게 **측정한 숫자와 함께** 보고해요. 숫자를 슬그머니 바꾸지 않아요.

## 13. 큰 KG의 표시

화면은 다른 리포지토리의 범위예요(ADR-0016). 여기서는 API가 **그룹과 수를 먼저 제공하는 것**만 정해요. `neighbors`의 `groups`, `containment`의 `children_summary`와 `subtree_size`, `impact`의 `counts_by_result`, `counts_by_type`, `counts_by_parent`가 그것이에요.

## 14. 알려진 한계와 가정

- 시간은 계산하지 않아요(UPS와 냉방의 지속 시간, 페일오버 시간, 수렴 시간). `delay_class` 표시만 해요. 부하가 이동하는 연쇄 과부하, 부분 성능 저하, 해시로 분산되는 ECMP의 쏠림도 계산하지 않아요. 엔벨로프의 `model_limits`에 문구가 붙어요.
- 구조와 상태는 KG에 있고 관측의 경과 시간이 응답에 붙어요. 수치 시계열은 KG에 쌓지 않아요. 소프트웨어 버그나 설정 오류는 모델링하지 않아요.
- 엔진은 스토리지가 멈추면 소스의 최신 신선도를 알 수 없고 마지막으로 수신한 값으로 계산해요. 실제보다 최신으로 보일 수 있어요. 루프에서 임계값 계수가 낙관적으로 틀린다는 말은 추론이고, 기준 구현과의 대조가 이를 확인하는 방법이에요.
- 숫자(한도, 차수, 시드 수, 홉 수, 합격선, 메모리 배수)는 모두 측정하지 않은 가정이에요.
- `volatile` 값에는 이력이 없고 "그 버전 시점" 조회에도 없어요. 값이 같아 기록하지 않은 레코드 때문에 신선도는 간격의 절반만큼 정밀도가 떨어져요(ADR-0009).
- 숨김 객체의 수를 감춰도 `dead`를 바꿔 가며 질의하는 추론은 완전히 차단하지 못해요(11절).

## 15. 미결 사항

- 과거 버전의 `impact`를 따로 로드하는 데 걸리는 시간과, 한 번에 로드해 둘 수 있는 과거 버전의 수.
- 노출됨을 상속시키는 링크 의미 목록(5.1의 표에서 어느 링크 의미가 `exposed_inherited`를 전파하나).
- 조각의 크기와 개수, 병합 주기의 예산 값(가정으로 기본값 표에 둬요). 재수집 작업을 MCP에서 생성할 수 있는 역할.
- 프로젝션 파일의 컬럼 형식. 호출자별 한도의 숫자, MCP 응답의 크기 상한. 푸시 방식의 형식과 필터링. 질의 언어를 공개할지는 사전 정의 질의로 다루지 못하는 질의가 쌓인 뒤에 다시 검토해요.

## 16. 관련 결정과 요구사항

- ADR-0010(이 문서의 결정), ADR-0015(`competency`), ADR-0016(REST API), ADR-0006(역할 4종, 정규 인코딩 해시), ADR-0007(채널, 링크 의미, 이중화 그룹, 근거 등급), ADR-0009(변경 시퀀스, 확정 버전, 스토리지), ADR-0011(가용성의 등급), ADR-0012(읽기 권한, 감사), ADR-0013(헬스 상태, `quality`, `health`), ADR-0014(정의의 계보).
- REQ-134(규모), REQ-140, REQ-141, REQ-142, REQ-143, REQ-144, REQ-145, REQ-146.
- 관련 문서: [엔진의 모양](query-engine-shape.md), [영향 범위를 손으로 따라가기](kg-queries-examples.md), [REST API 설계](rest-api.md), [역량 질문](competency-questions.md), [KG 모델](kg-model.md), [KG 저장과 버전](kg-storage.md), [KG 운영](kg-operations.md), [데이터 관측성](data-observability.md), [읽기 권한과 역할](access-control.md).
