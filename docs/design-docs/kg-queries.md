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

- 사람, 다른 조직의 시스템, 에이전트가 KG에 어떤 물음을 어떤 모양으로 할 수 있나.
- "이것이 죽으면 무엇이 죽나"를 어떻게 정확히 따지나. 이중화, 이어짐, 모르는 것을 어떻게 다루나.
- 묻는 쪽이 많아도 저장소가 멈춰도 답이 이어지게 하려면 무엇을 나누나. 제품 책임자의 숫자(1초, 수 초)를 어떻게 확인하나.

## 2. 한눈에

```
 사람·시스템 ──HTTP API──┐                    ┌── SQL로 답: find get neighbors(얕게)
 에이전트 ────읽기 전용 MCP─┼─ 닫힌 질의 열하나 ─┤        containment changes versions quality scopes
 다른 시스템 ──변경 목록 ──┘   + 봉투            └── 엔진으로 답: impact exposure paths (neighbors가 깊으면)
                                                          │
 KG 버전 공개 ──일꾼이 투영 파일을 만든다──▶ 질의 엔진(여러 벌) ── 메모리의 투영 + 가림 층
                                             저장소가 멈춰도 올려 둔 버전으로 답한다
```

| 정한 것 | 한 줄 |
|---|---|
| 질의 | 닫힌 목록 열하나. 질의 언어는 열지 않는다 |
| 봉투 | 버전, 신선도, 근거 등급, 잘림, 가림, 모델의 한계가 모든 답에 붙는다 |
| 영향 범위 | 기댐은 문턱으로, 이어짐은 길로 따진다. 사실만 쓴 답, 가정을 넣은 답, 모름을 따로 준다 |
| 지키는 법 | 느리지만 뻔한 기준 구현과 속성 기반 시험으로 엔진을 견준다 |
| 엔진 | 따로 뜨는 역할, 여러 벌, 투영은 파생물 |

## 3. 닫힌 질의 열하나

HTTP 길은 아직 없으므로 질의의 이름으로만 적어요. 칸 이름은 계약 단위(`core.contract`)가 그대로 쓸 이름이에요.

### 공통 입력

| 칸 | 뜻 |
|---|---|
| `graph_version` | 물을 KG 버전. 없으면 답하는 쪽이 아는 가장 새 버전. 답의 봉투에 쓴 버전이 적힌다 |
| `limit`, `cursor` | 쪽 넘김. `limit`의 기본 50, 상한 500은 가정이다. `cursor`는 버전과 정렬 키를 품어서, 쪽 도중에 새 버전이 공개돼도 같은 버전으로 이어진다 |
| `view` | `summary`(수와 묶음 먼저, 기본) 또는 `items`. MCP에서는 `detail`로 `brief`와 `full`을 고른다 |

### 질의마다

| 질의 | 입력 칸 | 출력 칸 | 한도와 쪽 넘김 |
|---|---|---|---|
| `find` | `object_type`(`include_subtypes`), `trait`, `attributes`(이름, 연산, 값의 목록. 연산은 `equals`, `in`, `prefix`, `range`), `identifier`(종류, 값), `text`, `domain`, `scope_id` | `counts_by_type`, `items`(`object_id`, `object_type`, `display_name`, `domain`, `matched_on`) | 인덱스로 답한다. `limit`/`cursor`. 가린 도메인의 객체는 나오지 않는다 |
| `get` | `object_id`나 `identifier`, `include`(`attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`) | 객체, 속성(`name`, `value`, `unit`, `grade`, `basis`), 식별자들, 유효한 버전 구간, `provenance_refs`(원본 기록의 참조와 규칙의 버전) | 한 객체. 링크는 수만 준다(`neighbors`로 읽는다) |
| `neighbors` | `object_id`, `link_types`, `meanings`, `direction`(`out`, `in`, `both`), `max_steps`, `peer_types` | `groups`(`link_type`, `meaning`, `direction`, `peer_type`, `count`)를 먼저, 그다음 `items`(`link_id`, `link_type`, `peer_object_id`, `peer_type`, `grade`) | `max_steps` 기본 1, 상한 3은 가정. 허브는 `groups`만 주고 `items`는 `limit`로 자른다 |
| `containment` | `object_id`, `direction`(`up`, `down`), `depth`, `child_types` | `ancestors`(건물, 방, 랙의 차례), `children_summary`(타입별 수), `subtree_size`, `items` | 깊이 기본 1. 자식은 쪽 넘김 |
| `paths` | `from_object_id`, `to_object_id`, `via_meanings`(기본 `connected_to`), `link_types`, `max_steps`, `top_k`, `dead`, `dead_links` | `reachable`(`yes`, `no`, `unknown`), `paths`(`steps`, `length`, `weakest_grade`), `found_count` | `top_k` 기본 3, 상한 10, `max_steps` 상한은 가정. 시간 예산을 넘기면 `truncated` |
| `impact` | `dead`, `dead_links`, `site_failure`, `dead_scope_id`, `mode`(`both`, `facts_only`, `with_assumptions`), `view` | 5.9절 | 시드 수 상한 1,000은 가정. 결과는 요약을 먼저 주고 `items`는 쪽 넘김 |
| `exposure` | `dead`(없어도 된다), `scope_id`, `target_object_id`, `cut_order_max` | `tight_groups`, `cut_sets` | 차수 기본 2, 상한 3은 가정. `target_object_id`가 있을 때만 `cut_sets`를 센다 |
| `changes` | `from_version`, `to_version`, `object_types`, `domain`, `scope_id`, `change_kinds`(`added`, `changed`, `retired`) | 타입별·종류별 수, 그다음 `items`(`object_id`나 `link_id`, `change_kind`, `changed_fields`, `graph_version`) | 버전의 바뀐 기록에서 바로 읽는다. 쪽 넘김 |
| `versions` | `since_version`, `limit` | 버전들(`graph_version`, `created_at`, `definition_version`, `raw_record_set_id`, `change_counts`, `is_head`, `projection_state`: `ready`, `building`, `none`) | 쪽 넘김 |
| `quality` | `scope_id`, `source_id`, `metric`, `graph_version` | 지표 값들. 지표의 정의는 [KG 운영](kg-operations.md)이 정한다 | 지표 수가 적다 |
| `scopes` | `scope_id`(없으면 목록) | 저장해 둔 범위의 정의(타입, 포함 뿌리, 거름)와 이 버전에서의 `count` | 쪽 넘김 |

`impact`와 `exposure`와 `paths`는 비용이 큰 질의예요. 질의마다 **비용 등급**(낮음: `get`, `find`, `versions`, `quality`, `scopes` / 중간: `neighbors`, `containment`, `changes`, `paths` / 높음: `impact`, `exposure`)과 시간 예산을 선언하고, 넘기면 잘라서 `truncated`로 알려요. 숫자는 재서 정해요.

## 4. 모든 답에 붙는 봉투

| 칸 | 뜻 |
|---|---|
| `graph_version` | 답한 KG 버전(`id`, `number`, `created_at`). 엔진이 옛 버전으로 답하는 중이면 그 옛 버전 |
| `served_by` | 어느 역할이 답했나(`role`: `api_sql`, `engine`), 엔진이면 투영을 올린 때 |
| `freshness` | `sources`(`source_id`, `last_complete_at`, `state`: `fresh`, `stale`), `oldest_age_seconds`. `stale`은 주기의 두 배를 넘긴 것(ADR-0011, 가정) |
| `evidence` | `min_grade`(쓰인 입력 중 가장 낮은 등급), `counts`(`fact`, `assumed`, `unknown`) |
| `completeness` | `total`, `returned`, `truncated`, `reason`(`limit`, `time_budget`, `size_budget`), `next_cursor` |
| `hidden` | `hidden_passed_count`: 답이 지난 가려진 객체의 수. 내용은 없다 |
| `model_limits` | 이 답이 모르는 것의 문구(시간 의존, 부하 연쇄, 부분 저하 등. 14절) |
| `warnings` | 예: `cycle_seen`(의존의 고리를 만났다) |
| `data` | 질의마다의 본문 |

지어낸 예시예요. 전원 A·B 서버에서 PDU 하나가 죽은 `impact`의 봉투예요.

```yaml
graph_version: {id: "kgv-7f3a21", number: 412, created_at: "2031-03-04T02:10:00Z"}
served_by: {role: engine, projection_loaded_at: "2031-03-04T02:14:30Z"}
freshness:
  oldest_age_seconds: 5400
  sources:
    - {source_id: "src-facility-sheet", last_complete_at: "2031-03-04T01:00:00Z", state: fresh}
evidence: {min_grade: assumed, counts: {fact: 41, assumed: 1, unknown: 0}}
completeness: {total: 2, returned: 2, truncated: false, reason: null, next_cursor: null}
hidden: {hidden_passed_count: 0}
model_limits: ["전원이 끊긴 뒤 UPS 배터리가 버티는 시간은 따지지 않는다"]
warnings: []
data: {facts_only: {dead: [srv-2], unknown: [srv-3]}, with_assumptions: {dead: [srv-2], unknown: []}}
```

## 5. 영향 범위의 뜻

### 5.1 링크의 뜻마다 번지는 법

번지는 규칙은 ADR-0007의 표(링크의 뜻 열 개)가 정해요. 여기서 다시 정하지 않고, 계산이 그 표를 어떻게 쓰는지만 적어요.

| 링크의 뜻 | 영향 범위 계산에서 |
|---|---|
| `runs_on`, `supplied_by`, `realized_by`, `requires`(`hard`) | 도착이 죽으면 출발이 죽는다(기댐 계산) |
| `requires`(`soft`) | 도착이 죽으면 출발은 위험해진다. 위험은 `at_risk`에만 오르고 더 번지지 않는다(15절) |
| `member_of` | 번지지 않는다. 묶음의 평가에 쓰인다 |
| `connected_to` | 번지지 않는다. 이어짐 계산의 간선이다 |
| `contained_in` | 도착 자체의 장애(`site_failure`)로 입력했을 때만 안의 것이 죽는다 |
| `managed_by` | 서비스에는 번지지 않는다. 관리 수단을 잃은 객체를 `lost_management`로 따로 준다 |
| `owned_by`, `record_only` | 계산에 쓰지 않는다. 투영에도 넣지 않는다 |

### 5.2 이중화 묶음의 세 값

`RedundancyGroup`(와 하위 타입 `Lag`, `HaCluster` 같은 것)은 `min_alive`를 가져요. 구성원 중 산 것 `a`, 모르는 것 `u`, 전체 `n`일 때:

| 조건 | 묶음의 값 |
|---|---|
| `a >= min_alive` | 살았다. `slack = a - min_alive`가 여유다 |
| `a + u < min_alive` | 죽었다 |
| 그 밖 | 모른다 |

모르는 구성원이 있어도 "안전하다"로 읽히지 않아요. 구성원이 산지 죽었는지는 **그 구성원의 기댐을 끝까지 따라가서** 정해요. 그래서 겉으로 이중화인데 같은 PDU에 매달려 있으면 한꺼번에 죽어요(6.1절).

`min_alive`에도 근거 등급이 있어요. `min_alive`가 모름이면 1부터 `n`까지 가능한 값을 모두 따져서, 모든 값에서 답이 같으면 그 답이고 다르면 모른다고 해요(6.5절). 구성원마다 무게가 있는 묶음은 값을 적을 자리만 있고, 무게로 따지는 세부는 15절에 남겨요.

### 5.3 기댐과 이어짐, 두 계산을 왜 가르나

| | 기댐 | 이어짐 |
|---|---|---|
| 물음 | 죽음이 문턱을 넘어 번지나 | 닿아야 하는 곳까지 길이 남아 있나 |
| 링크의 뜻 | 5.1의 첫 두 줄과 묶음 | `connected_to` |
| 방법 | 죽은 것에서 출발해 묶음마다 산 수를 센다. 죽음이 번지는 만큼만 본다 | 닿아야 하는 곳(`anchor`)에서 살아 있는 간선을 따라 닿는 것을 찾는다 |
| 틀리는 법 | 그래프가 순서가 있는 의존이면 맞다 | 고리가 있는 망에서 세기만 하면 낙관적으로 틀린다 |

이 문서가 가르는 까닭은 **조사 담당의 추론**이에요. 문헌으로 확인한 사실이 아니에요. 고리가 있는 망에서 두 스위치가 서로를 산 이웃으로 세면, 둘 다 위쪽과 끊겨도 서로 때문에 살아 있다고 읽는다는 거예요. 직접 따져 보면 그럴듯하지만, 그래서 말로 믿지 않고 **기준 구현과 대조하는 시험**으로 지켜요(7절). 6.4절에 손으로 따라간 예가 있어요.

두 계산은 번갈아 돌려요. 기댐이 죽인 스위치는 이어짐의 간선에서 빠지고, 이어짐이 끊은(닿지 못하는) 객체는 다시 기댐의 죽은 것에 들어가요. 더 늘지 않을 때 멈춰요. 죽은 것은 늘기만 하므로 반드시 멈춰요. `anchor`를 모델에 어떻게 적는지는 ADR-0007의 표준 모델이 정하고, 이 문서는 `anchor`라는 표시로 적어요(15절).

이어짐도 세 값이에요. 살아 있는 간선만으로 닿으면 `yes`, 살아 있는 간선과 모르는 간선을 다 써도 닿지 못하면 `no`, 그 밖은 `unknown`이에요.

### 5.4 여러 개가 함께 죽는 물음

`dead`는 객체 ID의 목록이에요. 시드를 합쳐서 한 번에 따져요. 죽음은 늘기만 하므로(단조) 시드의 순서에 답이 달라지지 않아요. `dead_links`는 죽은 링크, `site_failure`는 자리 단위의 사고(`contained_in`으로 안의 것이 죽는다), `dead_scope_id`는 저장해 둔 범위 전체예요. 가려진 객체의 ID를 알아도 가림 층이 다룬다(11절).

### 5.5 `exposure`: 하나만 더 죽으면

`dead`가 주어진 상태에서 **살았고 여유(`slack`)가 0인 묶음**을 골라요. 그 묶음의 산 구성원이 후보예요. 후보마다 "이것까지 죽으면" 새로 죽는 객체 수(`would_die_count`)를 내요. 후보는 모든 객체가 아니라 빡빡한 묶음의 구성원과 그 기댐의 위쪽이라 작아요. `dead`가 비어 있으면 지금 이미 여유가 0인 묶음의 목록이 돼요. 묶음이 아닌 단일 기댐은 모든 객체에 있으므로 `exposure`의 대상이 아니에요.

### 5.6 사실만 쓴 답, 가정을 넣은 답, 모름

| 답 | 입력을 읽는 법 |
|---|---|
| 사실만 쓴 답(`facts_only`) | `fact` 입력만 쓴다. `assumed` 입력은 **모름으로 둔다**(없는 것으로 보지 않는다) |
| 가정을 넣은 답(`with_assumptions`) | `assumed` 입력도 `fact`처럼 쓴다 |
| 모름(`unknown`) | 두 답 어디서든, `unknown` 입력 때문에 정할 수 없는 객체. 따로 준다 |

사실만 쓴 답에서 정해진 것(죽음, 삶)은 가정을 넣은 답에서도 같아요. 모름은 가정을 넣으면 정해질 수 있어요. 7절이 이것을 불변식으로 걸어요. 답의 `evidence.min_grade`는 쓰인 입력 중 가장 낮은 등급이에요.

### 5.7 고리의 처리

| 고리 | 처리 |
|---|---|
| 이어짐의 고리(스위치 패브릭 등) | 위 5.3의 길 찾기로 푼다. 고리 때문에 틀리지 않는다 |
| 의존의 고리(서로 기대는 서비스) | 죽음은 시드에서 번지므로 계산은 멈춘다. 고리를 만나면 `warnings`에 `cycle_seen`을 단다. 고리 안의 객체가 시드 밖의 원인 없이 죽지는 않는다 |

### 5.8 최소 절단

"무엇 둘셋이 함께 죽으면 `target_object_id`가 끊기나"는 일반적으로 어려운 문제예요(하위식을 공유하면 NP-hard라고 조사 담당이 일반 지식으로 적었고, 문헌은 열람하지 못했어요). 그래서 **작은 차수까지만** 세요. 기본 차수 2, 상한 3은 가정이에요. 차수 1은 단일 장애점, 차수 2는 쌍이에요. 후보는 공통 조상을 공유하는 쌍으로 좁혀요. 답에 `cut_complete_up_to`(어느 차수까지 다 셌나)를 적어서, 세지 않은 차수는 "없다"가 아니라 "세지 않았다"로 읽히게 해요.

### 5.9 `impact`의 답

| 칸 | 뜻 |
|---|---|
| `facts_only`, `with_assumptions` | 각각 `dead`, `at_risk`, `unknown`. 먼저 `counts_by_type`과 `counts_by_parent`(자리), 그다음 `items`(`object_id`, `object_type`, `via`: 직전 원인 하나) |
| `groups_touched` | 영향을 받은 묶음(`group_id`, `value`, `alive`, `unknown`, `n`, `min_alive`, `slack`) |
| `reachability` | 이어짐 계산에서 닿지 못하게 된 `anchor` 쪽 요약 |
| `lost_management` | 관리 수단을 잃은 객체 |

## 6. 손으로 따라가기

지어낸 예시예요. 값은 `A`(산), `D`(죽은), `U`(모름)로 적어요.

### 6.1 A·B 전원 중 PDU 하나가 죽음

`srv-1`, `srv-2`, `srv-3`은 전원 공급 장치(`PowerSupply`)를 둘씩 가져요. 각 쌍은 `min_alive = 1`인 묶음 `g-pwr-N`에 들고, 서버는 묶음에 `supplied_by`(power)로 기대요. `dead = [pdu-a]`예요.

| 서버 | 공급 장치의 PDU | 따라간 값 | 묶음 | 서버 |
|---|---|---|---|---|
| `srv-1` | `psu-1a`→`pdu-a`, `psu-1b`→`pdu-b` (모두 `fact`) | 1a=D, 1b=A | `a=1 >= 1`: A, `slack=0` | A |
| `srv-2` | `psu-2a`→`pdu-a`, `psu-2b`→`pdu-a` (배선이 같은 PDU에 몰림. `fact`) | 2a=D, 2b=D | `a+u=0 < 1`: D | D |
| `srv-3` | `psu-3a`→`pdu-a` (`fact`), `psu-3b`→`pdu-b` (**`assumed`**) | 3a=D, 3b=사실만 쓴 답에서 U, 가정을 넣은 답에서 A | 사실만: `a=0,u=1`: U. 가정: `a=1`: A | U / A |

답: 죽음은 `srv-2`예요. `srv-3`은 사실만 쓴 답에서 모름이고 가정을 넣은 답에서 살아요. `exposure`는 `g-pwr-1`의 `slack`이 0이라 `psu-1b`와 그 위쪽 `pdu-b`를 후보로 내요. 겉으로만 이중화인 `srv-2`는 공통 원인을 따라가서 찾았어요.

### 6.2 LAG의 구성원 하나가 죽음

`lag-7`(`Lag`)은 포트 `port-1`~`port-4`가 구성원이고 `min_alive = 2`(장비 설정의 최소 링크 수, `fact`)예요. `srv-9`가 `lag-7`에 `requires`(`hard`)로 기대요. 포트를 스위치에 어떤 뜻으로 잇는지는 ADR-0007이 정해요. 여기서는 포트 자체가 죽는 경우만 봐요.

| `dead` | `a` | `lag-7` | `srv-9` |
|---|---|---|---|
| `port-3` | 3 | A, `slack=1` | A |
| `port-3`, `port-4` | 2 | A, `slack=0` (`exposure` 후보 `port-1`, `port-2`) | A |
| `port-2`, `port-3`, `port-4` | 1 (`1+0 < 2`) | D | D |
| 위와 같고 `min_alive`를 1로 낮춤 | 1 (`1 >= 1`) | A | A |

마지막 줄이 "`min_alive`를 낮추면 죽는 것이 늘지 않는다"의 한 예예요.

### 6.3 HA 클러스터의 호스트 하나가 죽음

`ha-1`(`HaCluster`)은 호스트 `h1`, `h2`, `h3`이 구성원이고 `min_alive = 2`예요. `vm-a`, `vm-b`는 `ha-1`에 `runs_on`으로 기대요(HA로 옮겨 도는 VM은 호스트가 아니라 클러스터에 기댄다고 적어요). `svc-x`는 `vm-a`에 `requires`(`hard`), `vm-b`에 `requires`(`soft`)로 기대요.

| `dead` | `ha-1` | `vm-a`, `vm-b` | `svc-x` |
|---|---|---|---|
| `h1` | `a=2`: A, `slack=0` | A | A |
| `h1`, `h2` | `a=1 < 2`: D | D | D |
| `vm-b` | A | `vm-b` D | `at_risk` (soft) |
| `h1`, `min_alive`를 3으로 높임 | `a=2 < 3`: D | D | D |

첫 줄에서 `ha-1`은 여유가 0이라 `exposure`가 `h2`, `h3`을 후보로 내요. 호스트가 죽고 VM이 다시 뜨는 사이의 끊김과, 남은 호스트의 용량이 모자랄 수 있다는 점은 따지지 않아요. 봉투의 `model_limits`에 문구가 붙어요.

### 6.4 MLAG 짝의 스위치 하나가 죽음

`sw-a`, `sw-b`는 `mlag-1`(`min_alive = 1`)의 구성원이에요. `vsw-1`(논리 스위치)은 `mlag-1`에 `realized_by`로 기대요. `core-1`은 `anchor`예요. 이어짐 간선: `core-1`-`sw-a`, `core-1`-`sw-b`, `sw-a`-`sw-b`, `srv-1`-`sw-a`, `srv-1`-`sw-b`.

**(가) `dead = [sw-a]`.** 기댐: `mlag-1`의 `a=1 >= 1`이라 A, `vsw-1`도 A예요. 이어짐: 살아 있는 간선은 `core-1`-`sw-b`, `sw-b`-`srv-1`이고 `sw-b`, `srv-1`에 닿아요. 죽은 것은 `sw-a`뿐이에요. `exposure`는 `mlag-1`의 `slack=0`이라 `sw-b`를 후보로 내요.

**(나) `dead_links = [core-1-sw-a, core-1-sw-b]`**(코어 쪽 업링크 둘).

| 계산 | 결과 |
|---|---|
| 기댐만 (죽은 객체가 없으므로) | `sw-a`, `sw-b` A, `mlag-1` A, `vsw-1` A. 모든 것이 산다 |
| 세기만 (서로를 산 이웃으로 셈) | `sw-a`는 `sw-b`가, `sw-b`는 `sw-a`가 산 이웃이라 둘 다 A. 같은 오답 |
| 이어짐 (`core-1`에서 출발) | 살아 있는 간선 중 `core-1`에 닿는 것이 없다. `sw-a`, `sw-b`, `srv-1` 모두 `no` |
| 번갈아 돌림 | 닿지 못한 `sw-a`, `sw-b`가 죽은 것에 들어가 `mlag-1`의 `a=0`: D, `vsw-1` D |

(나)에서 기댐만 쓰면 낙관적으로 틀린 답이 나와요. 이것이 두 계산을 가르는 까닭이고, 7절의 시험이 이 모양의 입력으로 지켜요.

### 6.5 N+1 냉방 묶음에서 하나가 죽음

`cool-grp-1`은 방식 `n_plus_1`인 `RedundancyGroup`이고 구성원은 `crah-1`~`crah-3`, `min_alive = 2`예요. 홀의 서버 `srv-21`~`srv-23`은 `cool-grp-1`에 `supplied_by`(cooling)로 기대요.

| `dead` | `a` | `cool-grp-1` | 서버 셋 |
|---|---|---|---|
| `crah-2` | 2 | A, `slack=0` (`exposure` 후보 `crah-1`, `crah-3`) | A |
| `crah-2`, `crah-3` | 1 | D | D |

`exposure`는 후보마다 `would_die_count = 3`을 내요. 이제 `min_alive = 2`가 **`assumed`**(냉방 용량을 소스가 주지 않아 관리자가 적은 값)라고 해 봐요. 사실만 쓴 답에서 `min_alive`는 모름이라 1, 2, 3을 다 따져요. `dead = [crah-2]`에서는 `a=2`, `n=3`이에요. `min_alive`가 1이나 2면 A, 3이면 `a+u < 3`이라 D이므로 답이 갈려서 **모름**이에요. 가정을 넣은 답은 A예요. 그래서 `srv-21`~`srv-23`은 사실만 쓴 답에서 모름, 가정을 넣은 답에서 살아요.

## 7. 기준 구현과 속성 기반 시험

**기준 구현**은 느리지만 뻔한 계산이에요. 모든 객체를 "산 것"으로 놓고, 시드를 죽은 것으로 바꾼 뒤, 정의대로 모든 객체의 값을 다시 구하는 것을 더 바뀌지 않을 때까지 되풀이해요(세 값). 이어짐은 `anchor`에서 하는 너비 우선 탐색이에요. 엔진은 빨라야 하므로 다르게 만들고, **엔진의 답 = 기준 구현의 답**이어야 해요. 기준 구현은 `core.query`의 일부이고 엔진 포트의 계약 시험으로 함께 가요. 엔진을 다른 언어로 갈아 끼울 때도 같은 골든 파일과 같은 시험을 통과해야 해요.

| 불변식 | 뜻 |
|---|---|
| 엔진 = 기준 구현 | 임의로 만든 회로(AND, 묶음, 이어짐)와 고리가 있는 망, 지어낸 세계에서 답이 같다 |
| 단조 | 시드를 더하면 죽는 것이 줄지 않는다(`dead` 집합). 모름과 위험은 단조가 아니다 |
| 죽은 것을 더함 | 이미 죽은 것을 시드에 더해도 답이 같다. 시드의 순서와 객체 번호의 순열에 답이 달라지지 않는다 |
| `min_alive` | 낮추면 죽는 것이 늘지 않는다. 1이면 "하나라도"와 같고, `n`이면 "모두"와 같다 |
| 구성원 | 산 구성원을 더하면 죽는 것이 늘지 않는다 |
| 사실과 가정 | 사실만 쓴 답에서 죽음이나 삶으로 정해진 것은 가정을 넣은 답에서도 같다 |
| 고리 | 고리가 있는 무방향 망에서 이어짐이 너비 우선 탐색과 같다(6.4의 오답 모양을 포함) |
| 투영 | 같은 KG 버전이면 같은 투영 파일(내용 해시가 같다). 파일의 객체 수와 간선 수가 저장소의 수와 같다 |
| 갈아 끼우기 | 갈아 끼우는 중의 답은 옛 버전의 답이거나 새 버전의 답이다. 섞이지 않는다 |
| 답의 칸 | `graph_version`이 있고, `truncated`가 참이면 `next_cursor`나 이유가 있다 |

속성 기반 시험의 도구는 구현하는 변경에서 라이선스를 보고 정해요.

## 8. 투영

KG 버전 하나에서 만든 메모리 안의 그래프예요. 파생물이라 다시 만들 수 있고 고가용의 대상이 아니에요. 질의 엔진이 올려 영향 범위와 `exposure`와 길을 답해요.

| 들어가는 것 | 뜻 |
|---|---|
| 객체 표 | 안정 ID와 내부 번호의 대응, 타입 번호, 물러남 표시 |
| 뜻별 이웃 배열 | 링크의 뜻(`runs_on`, `requires`의 `hard`/`soft`, `supplied_by`의 자원별, `realized_by`, `member_of`, `contained_in`, `connected_to`, `managed_by`)마다 정방향과 역방향. 링크마다 근거 등급 |
| 묶음의 표 | 묶음 ID, 구성원, `min_alive`와 그 등급, 방식, 무게(있으면) |
| 타입과 ID의 대응 | 타입 번호 ↔ 타입 이름, 특성(`NetworkAttached`, `Powered` 등) |
| 가림에 쓸 표시 | 객체마다 주인 도메인 번호. 가린 도메인과 민감 속성의 규칙은 ADR-0012가 정하고, 엔진은 이 번호로 가린다 |
| `anchor` 표시 | 이어짐이 닿아야 하는 객체 |
| 목록 머리 | 소스마다 `last_complete_at`(신선도를 엔진 혼자 계산하려고) |

표시 이름 같은 문자열을 투영에 넣을지는 재서 정해요(15절). `owned_by`와 `record_only`는 넣지 않아요.

**파일 형식의 요구.** 특정 언어의 직렬화에 묶이지 않아요. 평평한 배열과 머리(형식 버전, KG 버전 ID, 그래프 해시, 섹션 표, 섹션마다와 전체의 내용 해시)로 이루어져요. 해시는 정규 바이너리 인코딩 위의 SHA-256이에요(ADR-0006). 순서는 모두 정해진 순서로 정렬해요. **자리(사이트) 단위로 나눠 올릴 수 있어야 해요.** 조각마다 해시가 있고, 조각 사이를 건너는 간선은 따로 섹션을 둬요. 후보는 평평한 컨테이너와 공개된 열 형식이며, 정하는 것은 구현하는 변경이에요.

**만드는 흐름.** KG 버전이 공개되면 → 투영 파일을 만든다 → 내용 해시를 적어 보관한다 → 엔진이 새 파일이 있음을 알고 받는다 → 해시를 확인한다 → 메모리에 올린다 → 새 질의를 새 쪽으로 돌린다 → 진행 중이던 질의가 끝나면 옛 것을 놓는다. 한 벌씩 차례로 해요. 갈아 끼우는 동안 한 벌이 옛 것과 새 것을 함께 들고 있으므로 메모리는 두 배가 필요하다고 보고 재서 확인해요(가정). 투영을 처음 만드는 규모와 걸리는 시간도 재요.

## 9. 질의 엔진이라는 역할

ADR-0006의 역할 셋 중 하나예요. 같은 코드에서 시작점만 달라요.

- **여러 벌.** 부하에 따라 벌 수를 늘려요. 각 벌이 자기 메모리에 투영을 들어요.
- **저장소가 멈춰도 답한다.** 올려 둔 투영으로 답을 잇고, 답의 봉투에 버전과 신선도(목록 머리의 시각으로 계산)가 적혀요. 새 버전은 받지 못하니 옛 버전으로 답하는 것이 보여요.
- **API와 엔진 사이의 계약.**

| 방향 | 칸 |
|---|---|
| API → 엔진 | 질의 이름, 인자, `graph_version`(없으면 올려 둔 가장 새 것), 부르는 쪽의 보는 범위(도메인과 민감 등급), 시간·크기 예산, 요청 ID |
| 엔진 → API | 봉투와 본문, 또는 오류(`version_not_loaded`+올려 둔 버전 목록, `budget_exceeded`+좁히는 법, `too_many_seeds`) |
| 엔진 → 조율 | 올려 둔 버전들, 준비 상태, 투영을 올린 때 |

- **무엇이 SQL로 가고 무엇이 엔진으로 가나.** 기본 갈래는 아래이고, 어디까지가 SQL로 되는지는 지어낸 규모에서 SQL(재귀)로도 구현해 재서 가려요. SQL이 목표를 맞추면 엔진의 범위가 줄 수 있어요.

| 질의 | 기본 갈래 | 재서 가르는 것 |
|---|---|---|
| `find`, `get`, `changes`, `versions`, `quality`, `scopes`, `containment` | SQL | 허브에서의 `containment` 깊이 |
| `neighbors` | 1~3걸음은 SQL | 몇 걸음부터 엔진으로 보내나 |
| `paths`, `impact`, `exposure` | 엔진 | 단순한 아래 방향 도달만이라면 SQL로 되는 크기 |
개발 환경에서는 한 프로세스에 역할 셋을 함께 띄워도 포트를 거쳐요.

## 10. 면 셋

### HTTP API

- **버전이 있는 계약.** 깨는 변경은 새 버전으로 내요. 계약은 `core.contract`가 쥐고, 길은 구현하는 변경에서 등록해요.
- **시스템 계정.** 사람이 아닌 시스템이 쓰는 계정이 같은 역할(ADR-0012)을 받아요.
- **부르는 쪽마다 한도.** 초당 요청 수, 동시 요청 수, 계산 예산(비용 등급별)을 계정마다 정해요. 기본값은 재서 정해요. 넘으면 오류가 기다릴 시간이나 좁히는 법을 알려요.
- **대량으로 읽기(`bulk_read`).** 범위 하나(`scope_id`)를 한 버전에서 통째로 읽는 길이에요. 낱개 질의를 수만 번 부르게 두지 않으려는 것이에요. 쪽 단위 흐름으로 내려받고, 같은 봉투가 붙고, 가림이 적용되고, 쓴 사람이 감사에 남아요(ADR-0012). 열한 질의가 아니라 따로 둔 길이에요.

### 읽기 전용 MCP

- 툴 목록은 닫힌 질의와 같은 이름이에요(`find` ... `scopes`). `bulk_read`는 에이전트에게 주지 않아요. 서버에는 쓰는 길이 없고, 툴 설명에 읽기 전용임을 밝혀요.
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
- **가져가기 방식을 먼저.** 변경 목록은 `changes`와 같은 모양이에요. 받는 쪽이 마지막으로 가져간 `graph_version`을 `from_version`으로 주면 그다음부터 줘요. 가림은 목록에도 적용돼요.
- **밀어 주기**는 뒤예요. 보내는 길은 포트 뒤에 두고, 받는 쪽마다의 거름과 전달 보장은 그때 정해요.

## 11. 가림 층

가림의 규칙(민감한 속성, 가린 도메인)은 [여러 조직이 함께 쓰는 KG](domains-and-access.md)와 ADR-0012가 정해요. 여기서는 영향 범위에만 닿는 것만 적어요.

- **계산은 전체 그래프에서 한다.** 부르는 쪽에 따라 계산이 달라지면 "죽는 것"이 틀려요. 가리는 것은 **답을 낼 때**예요.
- 영향 범위가 가려진 객체를 지나면, 그 객체는 `dead`/`at_risk`/`unknown` 목록에 나오지 않고 `hidden.hidden_passed_count`에 수로만 잡혀요. 내용도 타입도 주지 않아요. 가려진 객체 때문에 이 객체가 죽는다는 사실(`via`)은 "가려진 객체"로 적혀요.
- `paths`가 가려진 객체를 지나면 그 걸음은 "가려진 객체 하나"로 보여요.
- 가려진 객체의 ID를 부르는 쪽이 알고 있어 `dead`에 넣으면 "찾을 수 없음"과 같은 모양으로 답해요.
- 가림 층은 엔진 안에 있고, API가 엔진에 부르는 쪽의 보는 범위를 넘겨요(9절). 투영의 가림 표시는 도메인 번호예요.

## 12. 성능

요구는 **제품 책임자의 숫자**예요.

| 물음 | 요구 |
|---|---|
| 스위치의 ECMP·BGP·LACP를 다 고려한 영향 범위 | 1초 안에 |
| 설비 문제가 VM·앱·서비스에 미치는 범위 | 수 초 안에 |

넘겨받은 "3초"는 쓰지 않아요. ECMP·BGP·LACP는 경로표가 아니라 장비 설정 값에서 온 묶음의 `min_alive`로 따져요. **재지 않았어요.** 아래 순서로 지어낸 3천만 객체(링크는 가정으로 1억 안팎)에서 재요.

| 순서 | 잴 것 |
|---|---|
| 1 | SQL만으로 답하는 질의의 기준선(재귀로 구현한 영향 범위 포함). 투영이 필요한 범위를 숫자로 정한다 |
| 2 | 투영 만들기: 시간과 메모리. 가정의 불합격선은 10분, 서버 메모리의 40% |
| 3 | 영향 범위의 응답 p50/p95/p99. 허브, 깊은 체인, 고리, 큰 영향 범위, 묶음이 많은 입력, 동시 호출을 넣는다 |
| 4 | 버전이 바뀔 때 투영 갈아 끼우기: 두 벌 공존 메모리, 진행 중 질의에 주는 영향 |
| 5 | 응답 크기: 대표 MCP 호출이 상한 안에서 요약되는지 |
| 6 | 결정성: 순서를 섞고 일꾼 수를 바꿔도 같은 투영과 같은 답 |

성능 계산이 초당 수천만~1억 간선을 방문한다는 조사 담당의 짐작이 있는데, 이것은 **추정**이고 재기 전에는 근거로 쓰지 않아요. 안 되면 먼저 파라미터, 색인, 나눔을 고쳐 보고, 그래도 안 되면 제품 책임자에게 **잰 숫자와 함께** 말해요. 숫자를 슬그머니 바꾸지 않아요.

## 13. 큰 KG를 화면에 보이는 일
화면의 설계(그림, 레이아웃, 드릴다운)는 클로즈 베타의 일이에요. 여기서는 서버가 **묶음과 수를 먼저 주는 것**만 정해요. `neighbors`의 `groups`, `containment`의 `children_summary`와 `subtree_size`, `impact`의 `counts_by_type`과 `counts_by_parent`가 그것이에요. 한 화면에 수천 개를 그리지 않아도 되게 서버가 접어서 줘요. 알파의 화면은 이 답을 표와 트리로 보이는 만큼만 만들어요.

## 14. 알고 가는 한계와 가정
- 기댐은 정적이에요. 시간 의존(UPS 배터리가 버티는 시간, 페일오버 지연, 수렴 시간), 부하가 옮겨 가는 연쇄 과부하, 부분 성능 저하, 해시로 나뉘는 ECMP의 쏠림은 따지지 않아요. "영향 있음, 없음"이 아니라 위험 표시로만 둘 수 있고, 봉투의 `model_limits`에 문구가 붙어요.
- 소프트웨어 버그나 설정 오류는 인프라 KG로 모델링하지 않아요. 영향 범위는 "구조상 영향"이에요.
- 엔진은 저장소가 멈추면 새 소스의 신선도를 모르고, 투영을 올린 때의 값으로 계산해요. 실제보다 최신으로 보일 수 있어요.
- 이어짐과 기댐을 번갈아 돌리는 방식, 사실만 쓴 답에서 `assumed`를 모름으로 두는 읽기, `min_alive`가 모름일 때 가능한 값을 모두 따지는 읽기는 이 문서가 정한 읽는 법이에요. 시험으로 지켜요.
- 고리에서 문턱 세기가 낙관적으로 틀린다는 말은 추론이에요. 기준 구현과의 대조가 이를 확인하는 방법이에요.
- 숫자(한도, 차수, 시드 수, 걸음 수, 합격선, 메모리 배수)는 모두 재지 않은 가정이에요.

## 15. 아직 정하지 않은 것
- `anchor`(이어짐이 닿아야 하는 곳)를 표준 모델에서 어떻게 적나.
- 위험(`at_risk`)이 위로 번지는지, 여유가 0인 묶음에 기대는 객체를 위험으로 올리는지. 지금은 둘 다 올리지 않고 `groups_touched`와 `exposure`로 알려요.
- 무게(용량)로 따지는 묶음의 계산.
- 투영 파일의 열 형식과 만드는 역할(일꾼이 만든다는 것은 기본안이고, 개발 환경에서는 엔진이 만들 수 있다).
- 투영에 표시 이름을 넣을지(저장소가 멈췄을 때 이름이 필요하다는 이유와 메모리를 맞바꾼다).
- 저장해 둔 범위를 쓰는 길과 MCP에서의 문제 제보 길(ADR-0011이 문제 목록을 정한다).
- 부르는 쪽마다 한도의 숫자, MCP 답의 크기 상한.
- 밀어 주기의 형식과 거름.
- 그래프 질의 언어를 열지는 닫힌 질의가 못 덮는 물음이 쌓인 뒤에 다시 봐요.

## 16. 관련 결정과 요구사항
- ADR-0010(이 문서의 결정), ADR-0006(역할 셋, 정규 인코딩 해시), ADR-0007(링크의 뜻, 이중화 묶음, 근거 등급), ADR-0009(KG 버전, 저장소), ADR-0011(가용성의 등급, 신선도), ADR-0012(도메인, 가림, 감사).
- REQ-140, REQ-141, REQ-142, REQ-143, REQ-144, REQ-145.
- 이웃 문서: [KG 모델](kg-model.md), [KG 저장과 버전](kg-storage.md), [KG 운영](kg-operations.md), [여러 조직이 함께 쓰는 KG](domains-and-access.md).
