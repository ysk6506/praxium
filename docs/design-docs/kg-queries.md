---
title: KG에 묻기 — 닫힌 질의, 영향 범위, 투영과 엔진
kind: guide
status: active
follows:
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
---
# KG에 묻기

KG에 묻는 길 전체의 설계예요. 닫힌 질의 열하나, 모든 답에 붙는 봉투, 영향 범위의 뜻, 그것을 만드는 투영과 질의 엔진, 바깥으로 내는 면 셋을 정해요. 코드는 아직 없어요. 타입 이름은 영어이고, 표준 모델의 최종 이름은 ADR-0007의 표준 모델을 따라요. 이 문서의 이름은 예시예요.

## 1. 무엇에 답하나

- 누가 KG에 어떤 물음을 어떤 모양으로 할 수 있나. "이것이 죽으면 무엇이 어떻게 되나"를 이중화, 이어짐, 모르는 것까지 어떻게 정확히 따지나.
- 묻는 쪽이 많아도 저장소가 멈춰도 답이 이어지게 하려면 무엇을 나누나. 제품 책임자의 숫자(1초, 수 초)를 어떻게 확인하나.

## 2. 한눈에

```
 사람·시스템 ──┐                              ┌── SQL로 답: find get neighbors(얕게) containment
 에이전트(읽기 전용 MCP) ─┼─ API ─ 닫힌 질의 열하나 ─┤        changes versions quality scopes
 다른 시스템(변경 목록) ─┘        + 봉투         └── 엔진으로 답: impact exposure paths
                                                         │ (부르는 쪽은 엔진을 직접 부르지 않는다)
 KG 버전 공개 ─ 일꾼이 투영 파일을 만든다 ─▶ 질의 엔진(여러 벌) ─ 메모리의 투영 + 가림 층
                                            저장소가 멈춰도 올려 둔 버전으로 답한다
```

## 3. 닫힌 질의 열하나

HTTP 길은 아직 없으므로 질의의 이름으로만 적어요. 칸 이름은 계약 단위(`core.contract`)가 그대로 쓸 이름이에요.

### 공통 입력

| 칸 | 뜻 |
|---|---|
| `graph_version` | 물을 KG 버전의 ID. 없으면 답하는 쪽이 아는 가장 새 버전. 답의 봉투에 쓴 버전이 적힌다 |
| `limit`, `cursor` | 쪽 넘김. `limit`의 기본 50, 상한 500은 가정이다. `cursor`는 버전 ID와 정렬 키를 품어서, 쪽 도중에 새 버전이 공개돼도 같은 버전으로 이어진다 |
| `view` | `summary`(수와 묶음 먼저, 기본) 또는 `items`. MCP에서는 `detail`로 `brief`와 `full`을 고른다 |

### 질의마다

| 질의 | 입력 칸 | 출력 칸 | 한도와 쪽 넘김 |
|---|---|---|---|
| `find` | `object_type`(`include_subtypes`), `trait`, `attributes`(이름, 연산, 값의 목록. 연산은 `equals`, `in`, `prefix`, `range`), `identifier`(종류, 값), `text`, `domain`, `scope_id` | `counts_by_type`, `items`(`object_id`, `object_type`, `display_name`, `domain`, `matched_on`) | 인덱스로 답한다. `limit`/`cursor`. 가린 도메인의 객체는 나오지 않는다 |
| `get` | `object_id`나 `identifier`, `include`(`attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`) | 객체, 속성(`name`, `value`, `unit`, `grade`, `basis`), 식별자들, 유효한 버전 구간, `provenance_refs`(원본 기록의 참조와 규칙의 버전) | 한 객체. 링크는 수만 준다(`neighbors`로 읽는다) |
| `neighbors` | `object_id`, `link_types`, `meanings`, `direction`(`out`, `in`, `both`), `max_steps`, `peer_types` | `groups`(`link_type`, `meaning`, `direction`, `peer_type`, `count`)를 먼저, 그다음 `items`(`link_id`, `link_type`, `peer_object_id`, `peer_type`, `grade`) | `max_steps` 기본 1, 상한 3은 가정. 허브는 `groups`만 주고 `items`는 `limit`로 자른다 |
| `containment` | `object_id`, `direction`(`up`, `down`), `depth`, `child_types` | `ancestors`(건물, 방, 랙의 차례), `children_summary`(타입별 수), `subtree_size`, `items` | 깊이 기본 1. 자식은 쪽 넘김 |
| `paths` | `from_object_id`, `to_object_id`, `via_meanings`(기본 `connected_to`), `link_types`, `max_steps`, `top_k`, `dead`, `dead_links` | `reachable`(`yes`, `no`, `unknown`), `paths`(`steps`, `length`, `weakest_grade`), `found_count` | `top_k` 기본 3, 상한 10, `max_steps` 상한은 가정. 시간 예산을 넘기면 `truncated` |
| `impact` | `dead`, `dead_links`, `site_failure`, `dead_scope_id`, `mode`(`both`, `facts_only`, `with_assumptions`), `view` | 5.8절 | 시드 수 상한 1,000은 가정. 결과는 요약을 먼저 주고 `items`는 쪽 넘김 |
| `exposure` | `dead`(없어도 된다), `scope_id`, `target_object_id`, `cut_order_max` | `tight_groups`, `cut_sets` | 차수 기본 2, 상한 3은 가정. `target_object_id`가 있을 때만 `cut_sets`를 센다 |
| `changes` | `from_version`, `to_version`(버전 ID), `object_types`, `domain`, `scope_id`, `change_kinds`(`added`, `changed`, `retired`) | 타입별·종류별 수, 그다음 `items`(`object_id`나 `link_id`, `change_kind`, `changed_fields`, `graph_version`) | 버전의 바뀐 기록에서 바로 읽는다. 쪽 넘김 |
| `versions` | `since_version`(버전 ID), `limit` | 버전들(`graph_version`, `created_at`, `definition_version`, `raw_record_set_id`, `change_counts`, `is_head`, `projection_state`: `ready`, `building`, `none`) | 쪽 넘김 |
| `quality` | `scope_id`, `source_id`, `metric`, `graph_version` | 지표 값들. 지표의 정의는 [KG 운영](kg-operations.md)이 정한다 | 지표 수가 적다 |
| `scopes` | `scope_id`(없으면 목록) | 저장해 둔 범위(타입, 포함 뿌리, 거름), 만든 무리, 이 버전에서의 `count` | 쪽 넘김. 읽기만 한다. 만드는 길은 10절 |

`impact`와 `exposure`와 `paths`는 비용이 큰 질의예요. 질의마다 **비용 등급**(낮음: `get`, `find`, `versions`, `quality`, `scopes` / 중간: `neighbors`, `containment`, `changes`, `paths` / 높음: `impact`, `exposure`)과 시간 예산을 선언하고, 넘기면 잘라서 `truncated`로 알려요. 숫자는 재서 정해요.

## 4. 모든 답에 붙는 봉투

| 칸 | 뜻 |
|---|---|
| `graph_version` | 답한 KG 버전(`id`, `created_at`). 엔진이 옛 버전으로 답하는 중이면 그 옛 버전 |
| `served_by` | 어느 역할이 답했나(`role`: `api_sql`, `engine`), 엔진이면 투영을 올린 때 |
| `freshness` | 그 답에 쓰인 소스 중 **가장 나쁜 값**: `worst_source_id`, `oldest_age_seconds`, `state`(`fresh`, `stale`, `no_expected_period`). `stale`은 기대 주기의 두 배를 넘긴 것(ADR-0011, 가정) |
| `evidence` | `min_grade`(쓰인 입력 중 가장 낮은 등급), `counts`(`fact`, `assumed`, `unknown`) |
| `completeness` | `total`, `returned`, `truncated`, `reason`(`limit`, `time_budget`, `size_budget`), `next_cursor` |
| `hidden` | `hidden_passed_count`: 답이 지난 가려진 객체의 수. 내용은 없다 |
| `basis` | `configuration`: KG는 구성을 적고 그 순간의 상태(세션이 지금 올라와 있나)는 적지 않는다 |
| `model_limits` | 이 답이 모르는 것의 문구(14절) |
| `warnings` | 예: `capacity_group_unknown`(용량 묶음을 아직 따지지 않아 모름으로 줬다) |
| `data` | 질의마다의 본문 |

지어낸 예시예요. 6.1절의 `impact` 봉투예요.

```yaml
graph_version: {id: "kgv-7f3a21", created_at: "2031-03-04T02:10:00Z"}
served_by: {role: engine, projection_loaded_at: "2031-03-04T02:14:30Z"}
freshness: {worst_source_id: "src-facility-sheet", oldest_age_seconds: 5400, state: fresh}
evidence: {min_grade: assumed, counts: {fact: 41, assumed: 1, unknown: 0}}
completeness: {total: 3, returned: 3, truncated: false, reason: null, next_cursor: null}
hidden: {hidden_passed_count: 0}
basis: configuration
model_limits: ["전원이 끊긴 뒤 UPS 배터리가 버티는 시간은 따지지 않는다"]
warnings: []
data:
  facts_only: {down: [srv-2], exposed: [srv-1], unknown: [srv-3]}
  with_assumptions: {down: [srv-2], exposed: [srv-1, srv-3], unknown: []}
```

## 5. 영향 범위의 뜻

### 5.1 상태는 채널마다, 링크의 뜻은 채널로

한 객체의 상태는 "살았다/죽었다" 하나가 아니에요. 채널(`service`, `power`, `cooling`, `network`, `storage`, `management`)마다 `up`, `down`, `unknown`이에요. "서버는 켜져 있는데 망에서 끊겼다"와 "서비스는 도는데 관리할 길이 끊겼다"를 가르려는 것이에요. 링크의 뜻(열하나)은 "출발의 어느 채널이 도착의 어느 채널에 기대나"로 정의돼요. 정의는 ADR-0007이 쥐고, 계산이 쓰는 모양만 적어요.

| 링크의 뜻 | 계산에서 |
|---|---|
| `runs_on` | 출발의 `service` ← 도착의 `service`. 매개 값 `failover_to`(묶음)가 있으면 5.4절 |
| `requires`(`hard`/`soft`) | `service` ← `service`. soft는 죽지 않고 `degraded` |
| `supplied_by`(`power`/`cooling`/`network`/`storage`) | 출발의 그 채널 ← 도착의 `service` |
| `realized_by` | `service` ← `service` |
| `part_of` | 전체의 `service`가 죽으면 부품도 죽는다. 전체가 부품을 필요로 하는지는 `whole_needs_part`(`hard`/`soft`/`none`, 기본 `none`)나 이중화 묶음으로 |
| `contained_in` | 자리다. 자리 단위의 사고(`site_failure`)에서만 내용물로 번진다 |
| `connected_to` | 이어짐의 간선. 양 끝이 살아 있을 때만 쓴다. `network`의 길을 따질 때 쓴다 |
| `member_of` | 묶음의 구성원(매개 값 무게, 역할) |
| `managed_by` | 출발의 `management` ← 도착의 `service` |
| `owned_by`, `record_only` | 번지지 않는다. 투영에도 넣지 않는다 |

타입은 자기 `service`가 어느 채널을 필요로 하는지를 특성으로 선언해요(`Powered`는 `power`, `Cooled`는 `cooling`, `NetworkAttached`는 `network`, 스토리지를 쓰는 타입은 `storage`를 hard로. 타입이 강도를 바꿀 수 있어요). 링크에는 `delay_class`(`immediate`, `delayed`)가 있어요.

### 5.2 이중화 묶음

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

모르는 구성원이 있어도 "안전하다"로 읽히지 않아요. 구성원이 산지는 **그 구성원의 기댐을 끝까지 따라가서** 정해요. 겉으로 이중화인데 같은 PDU에 매달려 있으면 한꺼번에 죽어요(6.1절). 설정에서 끌어낸 ECMP, LACP, BGP의 묶음은 질의할 때 계산하지 않고 **받을 때 만들어 저장해요**. 설정을 온전히 읽었으면 `fact`예요(구성에 관한 사실). `min_alive`에도 근거 등급이 있고, 모르면 1부터 `n`까지 가능한 값 모두에서 답이 같을 때만 그 답이에요(6.5절).

### 5.3 기댐과 이어짐, 번갈아 최소 고정점까지

`network` 채널은 둘로 따져요.

| | 기댐 | 이어짐 |
|---|---|---|
| 물음 | 죽음이 묶음의 문턱을 넘어 번지나 | 닻까지 길이 남아 있나 |
| 대상 | 설정에서 끌어낸 묶음(LACP, ECMP, 짝을 이룬 스위치)과 5.1의 기댐 | `connected_to` 간선 위의 길. 닻은 특성 `NetworkAnchor`를 단 객체 |
| 방법 | 죽은 것에서 출발해 묶음마다 산 수를 센다 | 닻에서 살아 있는 간선을 따라 닿는 것을 찾는다 |
| 틀리는 법 | 순서 있는 의존이면 맞다 | 고리가 있는 망에서 세기만 하면 낙관적으로 틀린다 |

가르는 까닭은 **조사 담당의 추론**이에요. 문헌으로 확인한 사실이 아니에요. 고리가 있는 망에서 두 스위치가 서로를 산 이웃으로 세면, 둘 다 위쪽과 끊겨도 서로 때문에 살아 있다고 읽는다는 거예요. 그래서 말로 믿지 않고 **기준 구현과 대조하는 시험**으로 지켜요(7절).

계산은 **가장 작은 고정점**이에요. 처음에는 모두 살아 있고, 죽었다고 준 것에서 출발해 규칙이 강제하는 것만 죽어요. 기댐이 죽인 스위치는 이어짐의 간선에서 빠지고, 닿지 못하는 객체는 다시 기댐의 죽은 것에 들어가요. **번갈아 돌려 더 늘지 않을 때 멈춰요.** 죽은 것은 늘기만 하므로 반드시 멈춰요.

이어짐도 세 값이에요. 살아 있는 간선만으로 닿으면 `yes`, 살아 있는 간선과 모르는 간선을 다 써도 못 닿으면 `no`, 그 밖은 `unknown`이에요. **범위 안에 닻이 없으면 `unknown`이에요.** "안전"이 아니에요.

### 5.4 객체마다의 결과
| 결과 | 뜻 |
|---|---|
| `down` | `service`가 죽는다. `lost_channels`가 까닭을 말한다. 망만 잃었으면 "끊겨 있다"로 읽힌다 |
| `interrupted` | 죽지만 선언된 넘겨받기(`runs_on`의 `failover_to` 묶음)가 살아 있어 돌아올 것으로 본다. 묶음의 여유를 모르면 `unknown` |
| `degraded` | 살아 있지만 soft로 기대던 것을 잃었거나, 기대는 묶음의 구성원이 줄었다(여유는 남았다) |
| `exposed` | 살아 있지만 기대는 묶음의 여유가 0이다. 하나만 더 죽으면 죽는다 |
| `unmanageable` | `service`는 살아 있고 `management`만 잃었다 |
| `unknown` | 모르는 입력 때문에 정할 수 없다 |

시간은 따지지 않아요(냉방을 잃고 버티는 시간, 배터리가 버티는 시간, 넘겨받는 데 걸리는 시간). 링크의 `delay_class`가 `delayed`면 답에 "늦게 나타남"(`delay_class`)으로 표시만 해요. 버티는 시간 같은 값은 속성으로 받아 둬요.

### 5.5 여러 개가 함께 죽는 물음
`dead`는 객체 ID의 목록이에요. 시드를 합쳐서 한 번에 따져요. 죽음은 늘기만 하므로(단조) 시드의 순서에 답이 달라지지 않아요. `dead_links`는 죽은 링크, `site_failure`는 자리 단위의 사고(`contained_in`으로 안의 것이 죽는다), `dead_scope_id`는 저장해 둔 범위 전체예요.

### 5.6 `exposure`와 최소 절단
`exposure`는 `dead`가 주어진 상태에서 **살았고 여유(`slack`)가 0인 묶음**을 골라요. 그 묶음의 산 구성원이 후보예요. 후보마다 "이것까지 죽으면" 새로 `down`이 되는 객체 수(`would_die_count`)를 내요. 후보는 모든 객체가 아니라 빡빡한 묶음의 구성원과 그 기댐의 위쪽이라 작아요. `dead`가 비어 있으면 지금 이미 여유가 0인 묶음의 목록이에요. 묶음이 아닌 단일 기댐은 모든 객체에 있으므로 대상이 아니에요.

`target_object_id`를 주면 **최소 절단**("무엇 둘셋이 함께 죽으면 끊기나")을 세요. 일반적으로 어려운 문제라(하위식을 공유하면 NP-hard라고 조사 담당이 일반 지식으로 적었고 문헌은 열람하지 못했어요) **작은 차수까지만** 세요. 기본 2, 상한 3은 가정이에요. 후보는 공통 조상을 공유하는 쌍으로 좁혀요. 답의 `cut_complete_up_to`가 어느 차수까지 다 셌나를 적어서, 세지 않은 차수가 "없다"로 읽히지 않게 해요.

### 5.7 사실만 쓴 답, 가정을 넣은 답, 모름

| 답 | 입력을 읽는 법 |
|---|---|
| 사실만 쓴 답(`facts_only`) | `fact` 입력만 쓴다. `assumed` 입력은 **모름으로 읽는다**(없는 것으로 보지 않는다). `min_alive`가 모르면 가능한 값 모두에서 같을 때만 그 답 |
| 가정을 넣은 답(`with_assumptions`) | `assumed` 입력도 `fact`처럼 쓴다 |
| 모름(`unknown`) | 두 답 어디서든 `unknown` 입력 때문에 정할 수 없는 객체. 따로 준다 |

사실만 쓴 답에서 정해진 것은 가정을 넣은 답에서도 같아요. 모름은 가정을 넣으면 정해질 수 있어요. 7절이 불변식으로 걸어요. 고리(자기가 관리하는 클러스터 위에서 도는 제어부 같은)는 최소 고정점이라 **스스로 죽지 않아요.**

### 5.8 `impact`의 답

| 칸 | 뜻 |
|---|---|
| `facts_only`, `with_assumptions` | 각각 결과별 목록(`down`, `interrupted`, `degraded`, `exposed`, `unmanageable`, `unknown`). 먼저 `counts_by_result`, `counts_by_type`, `counts_by_parent`(자리), 그다음 `items` |
| `items` | `object_id`, `object_type`, `results`(여럿이 걸리면 모두), `lost_channels`, `delay_class`, `via`(직전 원인 하나) |
| `groups_touched` | 영향을 받은 묶음(`group_id`, `mode`, `value`, `alive`, `unknown`, `n`, `min_alive`, `slack`) |
| `reachability` | 범위 안의 닻 수, 닿지 못하게 된 객체 요약. 닻이 없으면 `network`는 `unknown` |

`impact`는 **시드 때문에 입력을 잃은 객체**를 보여 줘요. 시드가 없어도 원래 여유가 0인 묶음은 `exposure`가 보여 줘요.

## 6. 손으로 따라가기

지어낸 예시예요. 묶음의 값은 `A`(산), `D`(죽은), `U`(모름)로 적어요.

### 6.1 A·B 전원 중 PDU 하나가 죽음

`srv-1`, `srv-2`, `srv-3`은 전원 공급 장치(`PowerSupply`)를 둘씩 가지고, 각 쌍은 `min_alive = 1`인 묶음 `g-pwr-N`에 들어요. 서버는 묶음에 `supplied_by`(power)로 기대요. `dead = [pdu-a]`.

| 서버 | 공급 장치가 기대는 PDU | 따라간 값 | 묶음 | 서버의 결과 |
|---|---|---|---|---|
| `srv-1` | `psu-1a`→`pdu-a`, `psu-1b`→`pdu-b` (`fact`) | 1a 죽음, 1b 삶 | `a=1 >= 1`: A, `slack=0` | `exposed` |
| `srv-2` | `psu-2a`→`pdu-a`, `psu-2b`→`pdu-a` (배선이 한 PDU에 몰림. `fact`) | 둘 다 죽음 | `a+u=0 < 1`: D | `down`, `lost_channels: [power]` |
| `srv-3` | `psu-3a`→`pdu-a` (`fact`), `psu-3b`→`pdu-b` (**`assumed`**) | 3a 죽음, 3b는 사실만 쓴 답에서 모름, 가정을 넣은 답에서 삶 | 사실만: `a=0,u=1`: U. 가정: `a=1`: A | `unknown` / `exposed` |

`exposure`는 `g-pwr-1`의 `slack`이 0이라 후보로 `psu-1b`와 그 위쪽 `pdu-b`를 내요. 겉으로만 이중화인 `srv-2`는 공통 원인을 따라가서 찾았어요.

### 6.2 LAG의 구성원 하나가 죽음

`lag-7`(`Lag`, 방식 `count`)은 포트 `port-1`~`port-4`가 구성원이고 `min_alive = 2`(장비 설정의 최소 링크 수, 받을 때 만든 묶음이고 `fact`)예요. `srv-9`가 `lag-7`에 `supplied_by`(network)로 기대요. 범위에 닻 `core-1`(`NetworkAnchor`)이 있고 `srv-9`는 살아 있는 간선으로 닿아요(닻이 없으면 `unknown`). 포트를 스위치에 어떤 뜻으로 잇는지는 ADR-0007이 정해요. 여기서는 포트 자체가 죽는 경우만 봐요.

| `dead` | `a` | `lag-7` | `srv-9` |
|---|---|---|---|
| `port-3` | 3 | A, `slack=1` | `degraded` |
| `port-3`, `port-4` | 2 | A, `slack=0` (`exposure` 후보 `port-1`, `port-2`) | `exposed` |
| `port-2`, `port-3`, `port-4` | 1 (`1+0 < 2`) | D | `down`, `lost_channels: [network]` |
| 위와 같고 `min_alive`를 1로 낮춤 | 1 (`1 >= 1`) | A, `slack=0` | `exposed` |

마지막 줄이 "`min_alive`를 낮추면 죽는 것이 늘지 않는다"의 한 예예요.

### 6.3 HA 클러스터의 호스트 하나가 죽음

`ha-1`(`HaCluster`)은 호스트 `h1`, `h2`, `h3`이 구성원이고 `min_alive = 2`예요. `vm-a`는 `h1`에, `vm-b`는 `h2`에 `runs_on`으로 얹혀 있고 둘 다 `failover_to`가 `ha-1`이에요.

| `dead` | `ha-1` | `vm-a` | `vm-b` |
|---|---|---|---|
| `h1` | `a=2`: A, `slack=0` | `interrupted` (호스트는 죽었지만 넘겨받을 묶음이 산다) | `exposed` |
| `h1`, `h2` | `a=1 < 2`: D | `down`, `lost_channels: [service]` | `down` |
| `h1`, `min_alive`를 3으로 높임 | `a=2 < 3`: D | `down` | `h2`가 살아 있어 `down`이 아니다(넘겨받을 묶음만 죽었다) |
| `h1`, `min_alive`가 `assumed` (사실만 쓴 답) | 1, 2면 A, 3이면 D라 U | `unknown` (묶음의 여유를 모른다) | `unknown` |

첫 줄에서 `ha-1`은 여유가 0이라 `exposure`가 `h2`, `h3`을 후보로 내요. 호스트가 죽고 VM이 다시 뜨는 사이의 끊김과 남은 호스트의 용량은 따지지 않아요. `model_limits`에 문구가 붙어요.

### 6.4 MLAG 짝의 스위치 하나가 죽음

`sw-a`, `sw-b`는 `mlag-1`(`min_alive = 1`)의 구성원이에요. `vsw-1`(논리 스위치)은 `mlag-1`에 `realized_by`로, `srv-1`은 `mlag-1`에 `supplied_by`(network)로 기대요. `core-1`은 닻이에요. 이어짐 간선: `core-1`-`sw-a`, `core-1`-`sw-b`, `sw-a`-`sw-b`, `srv-1`-`sw-a`, `srv-1`-`sw-b`.

**(가) `dead = [sw-a]`.** 기댐: `mlag-1`의 `a=1 >= 1`이라 A, `slack=0`. 이어짐: 살아 있는 간선은 `core-1`-`sw-b`, `sw-b`-`srv-1`이고 `sw-b`, `srv-1`에 닿아요. 결과: `vsw-1`과 `srv-1`은 `exposed`, 잃은 채널은 없어요. `exposure`는 `sw-b`를 후보로 내요.

**(나) `dead_links = [core-1-sw-a, core-1-sw-b]`**(코어 쪽 업링크 둘).

| 계산 | 결과 |
|---|---|
| 기댐만 (죽은 객체가 없으므로) | `sw-a`, `sw-b` 삶, `mlag-1` A. 모든 것이 산다 |
| 세기만 (서로를 산 이웃으로 셈) | `sw-a`는 `sw-b`가, `sw-b`는 `sw-a`가 산 이웃이라 둘 다 삶. 같은 오답 |
| 이어짐 (`core-1`에서 출발) | `core-1`에 닿는 살아 있는 간선이 없다. `sw-a`, `sw-b`, `srv-1` 모두 `no` |
| 번갈아 돌림 | 닿지 못한 `sw-a`, `sw-b`가 `network`를 잃어 `down`. `mlag-1`의 `a=0`: D. `vsw-1`은 `down`(`lost_channels: [service]`), `srv-1`은 `down`(`lost_channels: [network]`) |

(나)에서 기댐만 쓰면 낙관적으로 틀린 답이 나와요. 이것이 두 계산을 가르는 까닭이고, 7절의 시험이 이 모양의 입력으로 지켜요.

### 6.5 N+1 냉방 묶음에서 하나가 죽음

`cool-grp-1`은 방식 `count`(`n_plus_1`)인 `RedundancyGroup`이고 구성원은 `crah-1`~`crah-3`, `min_alive = 2`예요. 홀의 서버 `srv-21`~`srv-23`은 `cool-grp-1`에 `supplied_by`(cooling)로 기대고, 이 링크의 `delay_class`는 `delayed`예요.

| `dead` | `a` | `cool-grp-1` | 서버 셋 |
|---|---|---|---|
| `crah-2` | 2 | A, `slack=0` (`exposure` 후보 `crah-1`, `crah-3`) | `exposed` |
| `crah-2`, `crah-3` | 1 | D | `down`, `lost_channels: [cooling]`, `delay_class: delayed`(늦게 나타남) |

`exposure`는 후보마다 `would_die_count = 3`을 내요. 이제 `min_alive = 2`가 **`assumed`**(냉방 용량을 소스가 주지 않아 관리자가 적은 값)라고 해 봐요. 사실만 쓴 답에서 `min_alive`는 모름이라 1, 2, 3을 다 따져요. `dead = [crah-2]`에서는 `a=2`, `n=3`이에요. `min_alive`가 1이나 2면 A, 3이면 `a+u < 3`이라 D이므로 답이 갈려서 **모름**이에요. 가정을 넣은 답은 A(`slack=0`)라 `exposed`예요. 그래서 서버 셋은 사실만 쓴 답에서 `unknown`, 가정을 넣은 답에서 `exposed`예요.

## 7. 기준 구현과 속성 기반 시험

**기준 구현**은 느리지만 뻔한 계산이에요. 모든 객체를 "산 것"으로 놓고, 시드를 죽은 것으로 바꾼 뒤, 정의대로 모든 객체의 채널 값을 다시 구하는 것을 더 바뀌지 않을 때까지 되풀이해요(최소 고정점, 세 값). 이어짐은 닻에서 하는 너비 우선 탐색이에요. 엔진은 빨라야 하므로 다르게 만들고, **엔진의 답 = 기준 구현의 답**이어야 해요. 기준 구현은 `core.query`의 일부이고 엔진 포트의 계약 시험으로 함께 가요. 엔진을 다른 언어로 갈아 끼울 때도 같은 골든 파일과 같은 시험을 통과해야 해요.

| 불변식 | 뜻 |
|---|---|
| 엔진 = 기준 구현 | 임의로 만든 회로(기댐, 묶음, 이어짐)와 고리가 있는 망, 지어낸 세계에서 답이 같다 |
| 단조 | 시드를 더하면 `down`인 것이 줄지 않는다. `unknown`과 `exposed`는 단조가 아니다 |
| 죽은 것을 더함 | 이미 죽은 것을 시드에 더해도 답이 같다. 시드의 순서와 객체 번호의 순열에 답이 달라지지 않는다 |
| 스스로 죽지 않음 | 시드가 없으면 아무것도 죽지 않는다. 의존의 고리 모양 입력도 같다 |
| `min_alive` | 낮추면 `down`인 것이 늘지 않는다. 1이면 "하나라도", `n`이면 "모두"와 같다 |
| 구성원 | 산 구성원을 더하면 `down`인 것이 늘지 않는다 |
| 사실과 가정 | 사실만 쓴 답에서 정해진 것은 가정을 넣은 답에서도 같다 |
| 고리 | 고리가 있는 무방향 망에서 이어짐이 너비 우선 탐색과 같다(6.4의 오답 모양을 포함). 닻이 없으면 `unknown` |
| 투영 | 같은 KG 버전이면 같은 투영 파일(내용 해시가 같다). 파일의 객체 수와 간선 수가 저장소의 수와 같다 |
| 갈아 끼우기 | 갈아 끼우는 중의 답은 옛 버전의 답이거나 새 버전의 답이다. 섞이지 않는다 |
| 답의 칸 | `graph_version`이 있고, `truncated`가 참이면 `next_cursor`나 이유가 있다 |

속성 기반 시험의 도구는 구현하는 변경에서 라이선스를 보고 정해요.

## 8. 투영

KG 버전 하나에서 만든 메모리 안의 그래프예요. 파생물이라 다시 만들 수 있고 고가용의 대상이 아니에요. 질의 엔진이 올려 영향 범위와 `exposure`와 길을 답해요. **일꾼이 만들어요.**

| 들어가는 것 | 뜻 |
|---|---|
| 객체 표 | 안정 ID와 내부 번호의 대응, 타입 번호, 물러남 표시 |
| 이름과 타입의 작은 표 | 표시 이름과 타입 이름(저장소가 멈춰도 답에 이름이 붙게). **민감한 속성은 넣지 않는다** |
| 뜻별 이웃 배열 | 링크의 뜻(`runs_on`과 `failover_to`, `requires`의 `hard`/`soft`, `supplied_by`의 채널별, `realized_by`, `part_of`와 `whole_needs_part`, `member_of`, `contained_in`, `connected_to`, `managed_by`)마다 정방향과 역방향. 링크마다 근거 등급과 `delay_class` |
| 묶음의 표 | 묶음 ID, 구성원, 방식(`count`, `capacity`), `min_alive`와 그 등급, 무게, 기대는 링크의 `demand` |
| 특성 표 | 객체의 특성(`Powered`, `NetworkAnchor` 등)과 필요로 하는 채널의 강도 |
| 가림에 쓸 표시 | 객체마다 주인 도메인 번호. 가린 도메인과 민감 속성의 규칙은 ADR-0012가 정하고, 엔진은 이 번호로 가린다 |
| 목록 머리 | 소스마다 `last_complete_at`과 기대 주기(신선도를 엔진 혼자 계산하려고) |

**파일 형식의 요구.** 특정 언어의 직렬화에 묶이지 않아요. 평평한 배열과 머리(형식 버전, KG 버전 ID, 그래프 해시, 섹션 표, 섹션마다와 전체의 내용 해시)로 이루어져요. 해시는 정규 바이너리 인코딩 위의 SHA-256이에요(ADR-0006). 순서는 모두 정해진 순서로 정렬해요. **자리(사이트) 단위로 나눠 올릴 수 있어야 해요.** 조각마다 해시가 있고, 조각 사이를 건너는 간선은 따로 섹션을 둬요. 열 형식의 후보는 평평한 컨테이너와 공개된 열 형식이며 정하는 것은 구현하는 변경이에요.

**만드는 흐름.** KG 버전이 공개되면 → 일꾼이 투영 파일을 만든다 → 내용 해시를 적어 **파일 보관 포트**에 올린다(어댑터: 파일 시스템이나 객체 저장소. PostgreSQL에 넣지 않는다) → 엔진이 새 파일이 있음을 알고 받아 자기 디스크에 둔다 → 해시를 확인한다 → 메모리에 올린다 → 새 질의를 새 쪽으로 돌린다 → 진행 중이던 질의가 끝나면 옛 것을 놓는다. 한 벌씩 차례로 해요. 갈아 끼우는 동안 한 벌이 옛 것과 새 것을 함께 들고 있으므로 메모리는 두 배가 필요하다고 보고 재서 확인해요(가정). 투영을 처음 만드는 규모와 시간도 재요.

## 9. 질의 엔진이라는 역할

ADR-0006의 역할 셋 중 하나예요. 같은 코드에서 시작점만 달라요.

- **여러 벌.** 부하에 따라 벌 수를 늘려요. 각 벌이 자기 메모리에 투영을 들어요.
- **저장소가 멈춰도 답한다.** 올려 둔 투영으로 답을 잇고, 봉투에 버전과 신선도(목록 머리의 시각으로 계산)가 적혀요. 새 버전은 받지 못하니 옛 버전으로 답하는 것이 보여요. 엔진은 버전을 번호가 아니라 버전의 ID로 알아봐요.
- **부르는 쪽은 엔진을 직접 부르지 않고 언제나 API를 거쳐요.** API는 저장소가 멈춰도 묻기의 길을 답해요. 로그인의 증표는 저장소 없이 확인하고, 무리와 역할의 짝은 나이에 상한이 있는 사본을 써요(ADR-0011, ADR-0012).
- **API와 엔진 사이의 계약.**

| 방향 | 칸 |
|---|---|
| API → 엔진 | 질의 이름, 인자, `graph_version`(없으면 올려 둔 가장 새 것), 부르는 쪽의 보는 범위(도메인과 민감 등급), 시간·크기 예산, 요청 ID |
| 엔진 → API | 봉투와 본문, 또는 오류(`version_not_loaded`+올려 둔 버전 목록, `budget_exceeded`+좁히는 법, `too_many_seeds`) |
| 엔진 → 조율 | 올려 둔 버전들, 준비 상태, 투영을 올린 때 |

- **무엇이 SQL로 가고 무엇이 엔진으로 가나.** 기본 갈래는 이래요. `find`, `get`, `changes`, `versions`, `quality`, `scopes`, `containment`는 SQL, `neighbors`는 1~3걸음까지 SQL, `paths`, `impact`, `exposure`는 엔진이에요. 어디까지가 SQL로 되는지는 지어낸 규모에서 SQL(재귀)로도 구현해 재서 가려요. SQL이 목표를 맞추면 엔진의 범위가 줄 수 있어요. 개발 환경에서는 한 프로세스에 역할 셋을 함께 띄워도 포트를 거쳐요.

## 10. 면 셋

### HTTP API

- **버전이 있는 계약.** 깨는 변경은 새 버전으로 내요. 계약은 `core.contract`가 쥐고, 길은 구현하는 변경에서 등록해요.
- **시스템 계정.** 사람이 아닌 시스템이 쓰는 계정이 같은 역할(ADR-0012)을 받아요.
- **부르는 쪽마다 한도.** 초당 요청 수, 동시 요청 수, 계산 예산(비용 등급별)을 계정마다 정해요. 기본값은 재서 정해요. 넘으면 오류가 기다릴 시간이나 좁히는 법을 알려요.
- **대량으로 읽기(`bulk_read`).** 범위 하나(`scope_id`)를 한 버전에서 통째로 읽는 길이에요. 낱개 질의를 수만 번 부르게 두지 않으려는 것이에요. 쪽 단위 흐름으로 내려받고, 같은 봉투가 붙고, 가림이 적용되고, 쓴 사람이 감사에 남아요(ADR-0012). 열한 질의가 아니라 따로 둔 길이에요.
- **쓰는 길 둘.** 저장해 둔 범위는 정의가 아니에요. 보기 역할이 API로 만들고, 만든 무리의 것이에요. KG가 틀렸다는 **문제를 알리는 길**도 API에 있고 문제 목록으로 들어가요(ADR-0011). 둘 다 KG를 바꾸지 않아요.

### 읽기 전용 MCP

- 툴 목록은 닫힌 질의와 같은 이름이에요(`find` ... `scopes`). `bulk_read`는 에이전트에게 주지 않아요. MCP는 **읽기만** 해요. 범위를 만들거나 문제를 알리는 길은 없고, 서버에는 쓰는 길이 없어요. 툴 설명에 읽기 전용임을 밝혀요.
- 답의 크기에 **상한**이 있고 넘으면 잘라서 더 좁게 묻는 법을 안내해요. 상한의 숫자는 가정이고 재서 정해요.
- **간단히와 자세히**(`detail`: `brief`, `full`). 기본은 간단히예요. 사람이 읽는 이름과 짧은 ID를 함께 줘요.
- **오류가 다음에 할 일을 말한다.** 예:

```yaml
error: {code: ambiguous_name, message: "이름이 같은 객체가 3개예요",
        next_step: "object_type이나 domain을 더해서 다시 물어요",
        candidates: [{object_id: "obj-31", object_type: "PhysicalServer", domain: "dom-a"}]}
```

### 변경 알림

- **알리는 것.** 새 KG 버전에서 바뀐 것: 객체와 링크의 더함, 고침, 물러남, 그리고 묶음(`min_alive`)의 변화. 버전의 바뀐 기록에서 나와요.
- **가져가기 방식을 먼저.** 변경 목록은 `changes`와 같은 모양이에요. 받는 쪽이 마지막으로 가져간 버전의 ID를 `from_version`으로 주면 그다음부터 줘요. 가림은 목록에도 적용돼요.
- **밀어 주기**는 뒤예요. 보내는 길은 포트 뒤에 두고, 받는 쪽마다의 거름과 전달 보장은 그때 정해요.

## 11. 가림 층

가림의 규칙(민감한 속성, 가린 도메인)은 [여러 조직이 함께 쓰는 KG](domains-and-access.md)와 ADR-0012가 정해요. 여기서는 영향 범위에만 닿는 것만 적어요.

- **계산은 전체 그래프에서 한다.** 부르는 쪽에 따라 계산이 달라지면 결과가 틀려요. 가리는 것은 **답을 낼 때**예요.
- 영향 범위가 가려진 객체를 지나면, 그 객체는 결과 목록에 나오지 않고 `hidden.hidden_passed_count`에 수로만 잡혀요. 내용도 타입도 주지 않아요. 그 객체 때문에 이 객체가 영향을 받는다는 사실(`via`)은 "가려진 객체"로 적혀요.
- `paths`가 가려진 객체를 지나면 그 걸음은 "가려진 객체 하나"로 보여요.
- 가려진 객체의 ID를 알고 `dead`에 넣으면 "찾을 수 없음"과 같은 모양으로 답해요.
- 가림 층은 엔진 안에 있고, API가 엔진에 부르는 쪽의 보는 범위를 넘겨요(9절). 투영의 가림 표시는 도메인 번호예요.

## 12. 성능

요구는 **제품 책임자의 숫자**예요.

| 물음 | 요구 |
|---|---|
| 스위치의 ECMP·BGP·LACP를 다 고려한 영향 범위 | 1초 안에 |
| 설비 문제가 VM·앱·서비스에 미치는 범위 | 수 초 안에 |

넘겨받은 "3초"는 쓰지 않아요. ECMP·BGP·LACP는 경로표가 아니라 장비 설정 값에서 받을 때 만든 묶음으로 따져요. **재지 않았어요.** 아래 순서로 지어낸 3천만 객체(링크는 가정으로 1억 안팎)에서 재요.

| 순서 | 잴 것 |
|---|---|
| 1 | SQL만으로 답하는 질의의 기준선(재귀로 구현한 영향 범위 포함). 투영이 필요한 범위를 숫자로 정한다 |
| 2 | 투영 만들기: 시간과 메모리. 가정의 불합격선은 10분, 서버 메모리의 40% |
| 3 | 영향 범위의 응답 p50/p95/p99. 허브, 깊은 체인, 고리, 큰 영향 범위, 묶음이 많은 입력, 동시 호출을 넣는다 |
| 4 | 버전이 바뀔 때 투영 갈아 끼우기: 두 벌 공존 메모리, 진행 중 질의에 주는 영향 |
| 5 | 응답 크기(대표 MCP 호출이 상한 안에서 요약되나)와 결정성(순서를 섞고 일꾼 수를 바꿔도 같은 투영, 같은 답) |

성능 계산이 초당 수천만~1억 간선을 방문한다는 조사 담당의 짐작은 **추정**이고 재기 전에는 근거로 쓰지 않아요. 안 되면 먼저 파라미터, 색인, 나눔을 고쳐 보고, 그래도 안 되면 제품 책임자에게 **잰 숫자와 함께** 말해요. 숫자를 슬그머니 바꾸지 않아요.

## 13. 큰 KG를 화면에 보이는 일

화면의 설계(그림, 레이아웃, 드릴다운)는 클로즈 베타의 일이에요. 여기서는 서버가 **묶음과 수를 먼저 주는 것**만 정해요. `neighbors`의 `groups`, `containment`의 `children_summary`와 `subtree_size`, `impact`의 `counts_by_result`, `counts_by_type`, `counts_by_parent`가 그것이에요. 알파의 화면은 이 답을 표와 트리로 보이는 만큼만 만들어요.

## 14. 알고 가는 한계와 가정

- 시간은 따지지 않아요(UPS와 냉방이 버티는 시간, 넘겨받는 시간, 수렴 시간). `delay_class` 표시만 해요. 부하가 옮겨 가는 연쇄 과부하, 부분 성능 저하, 해시로 나뉘는 ECMP의 쏠림도 따지지 않아요. 봉투의 `model_limits`에 문구가 붙어요.
- KG는 구성을 적고 순간의 상태는 적지 않아요. 답은 "구성 기준"이에요. 소프트웨어 버그나 설정 오류는 모델링하지 않아요.
- 엔진은 저장소가 멈추면 새 소스의 신선도를 모르고 투영을 올린 때의 값으로 계산해요. 실제보다 최신으로 보일 수 있어요.
- 고리에서 문턱 세기가 낙관적으로 틀린다는 말은 추론이에요. 기준 구현과의 대조가 이를 확인하는 방법이에요.
- 숫자(한도, 차수, 시드 수, 걸음 수, 합격선, 메모리 배수)는 모두 재지 않은 가정이에요.

## 15. 아직 정하지 않은 것

- 한 객체에 결과가 여럿 겹칠 때의 우선순위. 지금은 `results`에 모두 적어요.
- `impact`의 `exposed`와 `degraded`가 시드 때문에 잃은 입력에서만 나오는지(이 문서의 읽기), 넘겨받을 묶음(`failover_to`)의 여유 0도 `exposed`로 올리는지(6.3의 읽기).
- 투영 파일의 열 형식.
- 부르는 쪽마다 한도의 숫자, MCP 답의 크기 상한.
- 밀어 주기의 형식과 거름.
- 그래프 질의 언어를 열지는 닫힌 질의가 못 덮는 물음이 쌓인 뒤에 다시 봐요.

## 16. 관련 결정과 요구사항

- ADR-0010(이 문서의 결정), ADR-0006(역할 셋, 정규 인코딩 해시), ADR-0007(채널, 링크의 뜻, 이중화 묶음, 근거 등급), ADR-0009(KG 버전, 저장소), ADR-0011(가용성의 등급, 신선도), ADR-0012(도메인, 가림, 감사).
- REQ-140, REQ-141, REQ-142, REQ-143, REQ-144, REQ-145.
- 이웃 문서: [KG 모델](kg-model.md), [KG 저장과 버전](kg-storage.md), [KG 운영](kg-operations.md), [여러 조직이 함께 쓰는 KG](domains-and-access.md).
