---
title: ADR-0043 영향 범위 질의 impact — 전파 규칙, 이중화, 근거 등급, 한도와 업스트림
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - TRACE006
  - backend/internal/query/impact_power_test.go
  - backend/internal/query/impact_groups_test.go
  - backend/internal/query/impact_grades_test.go
  - backend/internal/query/impact_limits_test.go
  - backend/internal/readcheck/impact_scenarios_test.go
  - backend/internal/readcheck/impact_property_test.go
  - backend/cmd/praxium/impact_store_test.go
  - backend/internal/httpapi/impact_http_test.go
  - backend/internal/cli/impact_test.go
updates_docs:
  - AGENTS.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/kg-queries.md
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - docs/product-specs/requirements.md
  - harness/config/test-lock.json
  - harness/config/coverage-floor.json
  - docs/design-docs/adr/0007-kg-model.md
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
amends:
  - adr: "0007"
    what: 필요한 채널을 채우는 링크가 없으면 알 수 없음이 된다는 문장
  - adr: "0010"
    what: 영향 범위 응답의 필드와 한도를 정하지 않은 부분
decision_hash: f142cecd01c62691
---
# ADR-0043 영향 범위 질의 impact — 전파 규칙, 이중화, 근거 등급, 한도와 업스트림

## 맥락

ADR-0010이 영향 범위의 의미(링크 의미와 이중화 그룹의 3값, 사실만 쓴 응답과 가정 포함 응답, 알 수 없음)를 정했고, `core.query`에 기준 구현이 있다. 이 ADR은 그 기준 구현을 API와 명령으로 내놓으면서 설계가 비워 둔 곳을 정한다: 링크 의미마다 무엇을 넘기는지, 이중화를 어떻게 세는지, 약한 근거를 어떻게 읽는지, 계산을 어떻게 제한하는지, 반대 방향(무엇에 기대는가)을 어떻게 묻는지. 합성 세계(worldgen)에서 테스트하다 확인한 것도 함께 적는다.

## 결정

**1. 전파 규칙 (REQ-300).** 상태는 채널(`service`, `power`, `cooling`, `network`, `storage`, `management`)마다 `up`, `unknown`, `down`(`interrupted` 포함)의 3값이다. 모든 객체를 정상으로 놓고 실패한 객체에서 시작해, 바뀐 객체의 이용자만 다시 계산하는 라운드를 더 바뀌지 않을 때까지 반복한다(최소 고정점). 응답은 장애가 없을 때(기준선)와의 차이다. 그래서 순환 의존은 스스로 장애가 되지 않는다.

| 링크 의미 | 넘기는 것 |
|---|---|
| `runs_on` | 대상의 `service` 값을 넘긴다. `failover_to`가 선언돼 있고 대상이 실패했으면 그 그룹이 정한다: 맡을 수 있으면 `interrupted`, 맡을 수 없으면 `down`, 모르면 `unknown` |
| `requires` | `hard`는 `service`를 넘기고 `soft`는 넘기지 않는다(그 객체가 `degraded`가 된다) |
| `supplied_by` | 링크의 `resource` 채널(`power`, `cooling`, `network`, `storage`)에 대상의 `service`를 넘긴다. 같은 채널에 `supplied_by` 링크가 여럿이면 가장 나쁜 값이다(이중화는 그룹 객체로만 표현한다. kg-model 7.1) |
| `realized_by` | 대상의 `service`를 넘긴다 |
| `part_of` | 부분이 전체를 따른다(부분 `service` ← 전체 `service`). 전체가 부분을 필요로 하는지는 링크의 `whole_needs_part`(`hard`: 넘긴다, `soft`: 전체가 `degraded`, `none`: 없다)가 정한다 |
| `member_of` | 그룹의 판정에만 쓴다(2번) |
| `managed_by` | `management` 채널만 넘긴다. 잃어도 `service`는 정상이고 결과는 `unmanageable`이다 |
| `contained_in` | 넘기지 않는다. `site_failure`로 시드를 고를 때만 담긴 객체를 모은다 |
| `connected_to` | 따라가지 않는다. 연결성(앵커에서 도달 가능한가)은 계산하지 않고 응답의 `model_limits`에 적는다 |

시드는 세 가지다: `dead`(객체 실패), `site_failure`(객체와 담긴 객체 모두 실패), `dead_links`(링크가 끊김. 링크는 그래프에 남고 끊긴 채로 `down`을 넘기며 양 끝을 다시 계산한다). 끊긴 링크도 기준선에는 살아 있다.

**2. 이중화 (REQ-301).** 그룹은 구성원 수 `n`과 정상 구성원 수 `alive`, 모르는 구성원 수 `unknown`으로 3값을 낸다. `min_alive` `k`를 알면 `alive >= k`면 정상(여유 `alive - k`), `alive + unknown < k`면 장애, 그 밖은 알 수 없음이다. `k`를 모르면 `1..n`의 모든 값에서 같을 때만 값을 낸다. 용량 모드(`capacity`)의 그룹은 구성원이 모두 정상일 때만 정상이고 아니면 알 수 없음과 경고 `capacity_group_unknown`을 준다(용량을 계산하지 않으므로 낙관도 비관도 하지 않는다). 그룹이 정상으로 남아도 구성원이 줄었으면 그룹에 기대는 객체에 노출됨을 알린다: 여유가 0이 되면 `exposed_direct`, 거기에 기대는 객체는 `exposed_inherited`, 부분 구성이 줄었으면 `degraded`. 영향받은 그룹은 `groups_touched`에 `alive`, `unknown`, `n`, `min_alive`, `slack`과 함께 나온다.

**3. 근거 등급 (REQ-302).** 읽기가 둘이다. 사실만 쓴 응답(`facts_only`)은 `assumed` 링크를 알 수 없음으로 읽고, 가정 포함 응답(`with_assumptions`)은 `fact`처럼 쓴다. 약한 링크(가정, 알 수 없음, `max_staleness`를 넘은 관측)는 장애를 넘기지 않고 알 수 없음만 넘긴다: 약한 링크의 대상이 정상이면 그대로 정상이고, 정상이 아니면 알 수 없음이다. 이렇게 하지 않으면 장애와 상관없는 약한 링크가 기준선에서 객체를 알 수 없음으로 만들고, 실제 영향을 가린다(만들어 본 구현에서 알 수 없음이 수백 개 늘어나는 것으로 확인했다). 사실만 쓴 응답에서 확정된 결과는 가정 포함 응답에서도 같다(속성 테스트). 어떤 항목이 가정에만 기대면 `assumption_based`와 `weakest_grade`를 붙인다. `mode: both`가 기본이고 한쪽만 물으면 한쪽만 계산한다.

**4. `supplied_by` 링크가 없는 채널 (REQ-303). kg-model 6절을 고친다.** 타입이 필요하다고 선언했지만 그 채널을 채우는 `supplied_by` 링크가 없는 객체를 알 수 없음으로 읽지 않는다. 항목마다 `unfilled_channels`로 나열하고, 결과 요약에 `unfilled_channel_objects`(채널이 빈 객체의 수)를 센다. 명령의 요약 줄에도 나온다. 이유: 합성 세계에서는 냉각 링크가 없는 장비가 많아서(스위치 등) 알 수 없음으로 읽으면 전원 하나가 실패할 때 수백 개가 알 수 없음이 되어 진짜 알 수 없음이 묻힌다. 빈 채널은 장애와 무관한 데이터의 빈 곳이므로, 수로 알려 사용자가 빈 곳을 알게 하되 결과를 흐리지 않는다. 이는 "채우는 링크가 없으면 알 수 없음"이라는 kg-model 6절의 문장과 다르므로 ADR-0007의 그 부분을 고치고 문장을 같은 변경에서 바꾼다.

**5. 업스트림 (REQ-305).** `direction: upstream`과 `target_object_id`는 대상이 기대는 객체(의존 그래프를 거슬러 닿는 객체)를 구한다. 후보마다 그 하나만 실패시켜 대상의 결과를 두 읽기에서 계산하고, 대상이 `down`이 되면 `single_point_of_failure`다. 후보는 가까운 홉부터 2,000개까지 시도하고, 넘으면 `computation.items_over_cap`으로 알린다. 새 질의 이름을 만들지 않고 `impact`의 방향으로 둔다(질의 13종을 늘리지 않는다).

**6. 한도와 완전성 (REQ-304).** 요청마다 `max_hops`(0은 제한 없음), `max_items`(기본 5000, 상한 10만), `time_budget`(기본 30초, ISO 8601 기간)이 있다. 라운드 사이에 시간과 홉을 확인하고, 걸리면 그 라운드까지의 결과를 주되 `computation.complete: false`, `stopped_by`, `result_complete.reason`을 싣는다. 걸린 결과는 하한이다(더 번질 수 있다). `max_items`를 넘으면 항목은 자르되 `counts_*`는 전체를 센다(`items_over_cap`). 시드는 1,000개까지다. 페이지네이션은 두 읽기의 심각도 순서 키에 대한 커서이고 기존 질의와 같은 규칙(변경 번호에서 이어 읽기)을 따른다.

**7. 거절 (REQ-304).** 시드가 없거나 1,000개를 넘으면, 방향과 어긋난 필드(업스트림에 `dead`, 다운스트림에 `target_object_id`)면, 없거나 읽을 수 없는 객체나 링크면, `dead_scope_id`(저장된 범위가 아직 없다)면 고정된 코드로 거절한다. 읽을 수 없는 객체는 없는 객체와 같은 오류다. 읽기 권한 필터를 거치므로 경로의 숨김 단계는 `hidden`으로 나오고 수는 숨김을 뺀다.

**8. 인터페이스 (REQ-306).** `POST /v1/kgs/{kg}/queries:impact`와 `praxium query impact`. 계약 조각, 합쳐진 `openapi.json`, 서버, 웹 생성 코드가 일치하고 명령은 ADR-0041의 도움말 체계를 따른다. 사람용 출력은 요약, 영향 종류별 묶음, 경로 순서이고 `--json`은 데이터 그대로다.

## 이유

기준 구현은 느리지만 의미가 분명해야 한다는 ADR-0010의 방침을 따르되, 합성 세계에서 쓸 수 있는 속도(아래 측정)로 만들었다. 약한 링크가 알 수 없음만 넘기게 한 것과 빈 채널을 수로 보여 주는 것은, 모르는 것을 숨기지 않으면서 결과를 쓸 수 있게 하려는 선택이다. 업스트림을 방향으로 둔 것은 같은 계산을 거꾸로 부르는 것이라서 별도 계산이 필요 없기 때문이다.

## 대안과 기각 사유

- 빈 채널을 알 수 없음으로 읽기(kg-model 6절의 원래 문장): 합성 세계에서 실제 영향이 알 수 없음에 묻힌다. 기각.
- 약한 링크를 장애도 넘기는 것으로 읽기: 가정 포함 응답이 사실만 쓴 응답을 넘어서 확정이 뒤집힌다. 기각.
- 업스트림을 별도 질의로: 질의 13종을 늘리고 같은 계산을 두 번 구현한다. 기각.
- 연결성(`connected_to`)을 이번에 포함: 앵커와 루프 처리의 설계가 따로 필요하다. 이번 범위 밖(REQ-141의 남은 몫).

## 결과

**측정** (합성 세계 시드 7, 메모리 스토어, 개발 컨테이너, 요청마다 의존 그래프를 만든다. 프로젝션이 없는 기준 구현의 값이다):

| 질문 | 규모 2만 | 규모 20만 |
|---|---|---|
| 세계 적재 | 약 5초 | 약 46초 |
| UPS 하나 실패(`facts_only`) | 0.43초, 3,547개 변경 | 6.1초, 약 3.5만개 변경 |
| 수전반 하나 실패 | 0.28초 | 5.2초 |
| 분전반(RPP) 하나 실패 | 0.36초 | 1.8초 |
| 방 하나 `site_failure` | 0.47초, 약 7천개 | 2.9초, 약 1.4만개 |
| 서버의 업스트림 | 0.53초, 43개, 단일 장애점 5개 | 6.7초, 43개, 6개 |
| 응답 크기, 항목 1,000개(경로 포함) | 1.4~2.7MB | 1.6~3.0MB |
| `max_items: 100` | 0.2~0.3MB, 개수는 전체 | 0.2~0.65MB, 개수는 전체 |
| `max_hops: 2` | 1.4KB~10KB, `complete: false` | 1.4KB~20KB, 같음 |

시간은 대부분 요청마다 그래프를 만드는 데 쓴다(`max_hops: 2`도 20만 규모에서 1.4~4.6초). `time_budget`은 데모 서버에서 확인했다: 매우 짧은 예산이면 `complete: false`, `stopped_by: time_budget`, `result_complete.reason: time_budget`, 반환 0개로 응답하고 충분한 예산이면 완전하다. 이 수치로 REQ-142(1초, 수 초)를 충족했다고 말하지 않는다: 규모 2만은 1초 안이지만 20만은 아니고, 연결성이 빠져 있다. 프로젝션으로 그래프 구축을 요청에서 빼는 것이 다음 일이다(kg-queries 12절).

**HA 쌍이 랙 PDU 하나에 함께 내려가는 이유.** 생성기는 스토리지 어레이가 같은 랙의 PDU a와 b에 `POWERED_BY` 링크를 각각 직접 걸게 하고 `PowerRedundancyGroup`을 두지 않는다. 컨트롤러와 HA 쌍은 어레이의 `PART_OF`라 자기 전원 경로가 없다. 모델 의미(kg-model 7.1)로는 이중화가 그룹 객체로만 존재하고 같은 채널의 `supplied_by` 링크 여럿은 가장 나쁜 값을 따르므로, 두 PDU는 어레이에 직렬이다. 한 PDU가 어레이와 두 컨트롤러와 HA 쌍을 함께 내린다(서버는 PSU가 이중화 그룹이라 남는다). 엔진의 오류가 아니라 생성기가 어레이의 이중 전원을 그룹 없이 표현한 것이다. `TestImpactHAPairOfAnArrayGoesDownWithOnePduBecauseTheArrayIsWiredToBothInSeries`가 정답 데이터에서 두 컨트롤러의 전원 경로를 따라가 이를 증명한다. 생성기는 이 ADR에서 바꾸지 않는다. 생성기가 어레이에 전원 이중화 그룹을 두면 이 테스트가 깨지므로 그때 같이 고친다.

**인메모리 질의 엔진과의 대조 계획.** 이 구현이 ADR-0010의 기준 구현이다. 프로젝션을 쓰는 질의 엔진이 생기면 같은 합성 세계와 같은 시드로 두 엔진을 돌려 항목 단위로 대조하는 속성 테스트(엔진 = 기준 구현)를 만든다. 스토어는 메모리, 파일, PostgreSQL이 같은 답을 내는 것을 이미 테스트한다(`impact_store_test.go`). 질의 패키지는 worldgen을 가져오지 않고, 스토어 동등성 테스트은 `cmd/praxium`에 둔다.

**남은 것.** 연결성과 `exposure`, 시간(UPS 지속 시간, 페일오버 시간), `dead_scope_id`, 응답의 `data_health`와 `reachability`. 모두 `model_limits`와 kg-queries 14절에 적는다.
