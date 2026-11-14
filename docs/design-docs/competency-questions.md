---
title: 역량 질문과 충족률 — KG가 답해야 하는 질문을 정의로 적고 객체별로 평가한다
kind: guide
status: active
follows:
  - docs/design-docs/adr/0015-competency-questions.md
---
# 역량 질문과 충족률

"이 KG가 답할 수 있어야 하는 질문"을 정의로 적고, 그 질문에 지금 답할 수 있는지를 객체별로 따져 비율로 산출하는 설계예요. 코드는 아직 없어요(백엔드는 Go. ADR-0006). 타입, 속성, 링크 타입의 이름은 영어예요. 이 문서의 이름과 숫자는 가상의 예시예요.

## 1. 목적과 다루는 질문

- 수집한 데이터로 "이 서버는 전원이 이중인가" 같은 질문에 답할 수 있나. 어디가 비어 있고 왜 비어 있나.
- 그 계산을 사람이 실행하지 않아도 되려면, 여러 홉을 따라가는 질문까지 어떻게 증분 갱신하나.
- 충족률을 사이트별, 환경별, 담당 조직별로 어떻게 필터링해서 보나.

## 2. 개요

역량 질문(competency question)은 KG를 설계할 때 범위를 정하고 완료 여부를 확인하는 데 쓰는 방법이에요. 보통은 글로 적고 사람이 확인해요. 프락시움은 **KG가 지금 답할 수 있는지를 객체별로 따져요.**

| | 관측성(ADR-0013) | 역량 질문(이 문서) |
|---|---|---|
| 확인하는 것 | 데이터가 잘 들어오나 | 그 데이터로 질문에 답할 수 있나 |
| 단위 | 소스, 피드, 타입, 속성 | 질문 하나와 그 대상 객체 각각 |
| 문제가 될 때 | 소스 장애, 스키마 변경, 오래됨(stale) | 링크가 없음, 속성이 없음, 체인이 끊김 |
| 둘의 관계 | 헬스 상태가 비정상이면 충족률이 떨어지는 이유가 돼요 | 충족률이 낮으면 헬스 상태의 목표로 알려요 |

```
 역량 질문(정의) ─ 대상 + 필요한 것(패턴) + 목표 + 추적할 그룹
 KG의 변경(+ volatile의 경량 변경 레코드) ─▶ 적용 트랜잭션 밖의 소비자: 영향받은 (질문, 대상)만 다시 계산 ─▶ 객체별 충족 표시 ─▶ 충족률 2종
 사전 정의 질의에 의존하는 필요 ◀─ 질의 엔진(알파는 주기)      조회할 때: 표시를 집계해 응답해요(구조 질문은 표시 없이 집계해요)
```

## 3. 역량 질문의 필드

정의의 항목이라서 바뀌지 않는 `id`가 있고 이름을 바꿔도 이어져요(ADR-0014).

| 필드 | 설명 |
|---|---|
| `question_id`, `name` | 바뀌지 않는 `id`와 `snake_case` 이름 |
| `text` | 사람이 읽는 질문(텍스트). 언어별 표시 이름 필드는 따로 둬요 |
| `assignees` | 담당자(사람이나 그룹). 연락처예요. 권한이 아니에요(ADR-0012) |
| `origin` | `standard`, `structural`, `user`, `suggested`(9절). 고정 목록이라 새 값은 결정 기록으로 추가해요 |
| `target` | 대상: `object_type`(`include_subtypes`)와 `where`(4.4절의 패턴) |
| `need` | 필요한 것: 대상 객체 하나가 이 질문에 답하려면 KG에 있어야 하는 것(4절) |
| `objective` | 목표 충족률(선택). 사실만 읽기에 적용하는지 가정 포함 읽기에 적용하는지도 적어요(10.2절) |
| `block_on_regression` | 켜면 목표 아래에서 지금보다 더 낮추는 제안도 차단해요(선택, 기본 꺼짐. 10.1절) |
| `tracked_groupings` | 미리 적어 두고 시계열로 쌓을 그룹의 목록(선택). 항목은 `grouping_id`와 `group_by`(7절). 예: 사이트별, 환경별 |

## 4. 필요한 것의 패턴

질의 언어를 공개하지 않아요. 고정된 종류를 트리로 이어 붙여요. 단계 수에 제한이 없어요. 노드마다 질문 안에서 유일한 `need_id`가 있어서 충족하지 못한 지점을 이름으로 가리켜요.

### 4.1 종류

| `kind` | 필드 | 충족 조건 |
|---|---|---|
| `attribute_present` | `attribute`, `min_grade`, `max_age` | 값이 있고 등급이 `min_grade` 이상이며 관측 시각이 `max_age` 안이에요 |
| `link_present` | `link_type`이나 `meaning`, `direction`(`out`, `in`), `peer_type`(타입이나 특성), `min_count`(기본 1), `min_grade`, `view`(`as_designed`, `as_observed`. 기본 `as_observed`), `peers`, `distinct_at`, `then` | 조건에 맞는 링크가 `min_count`개 이상이에요. `then`이 있으면 **그 도착 객체가 `then`을 채운 것만** 세요 |
| `follow` | `link_type`이나 `meaning`, `direction`, `until`(도달할 타입이나 특성), `max_steps`, `min_grade`, `then` | 링크를 따라가다 `until`에 도달해요. 홉 수를 미리 알 수 없어요. `max_steps` 안에 도달하지 못하면 충족하지 못한 거예요 |
| `group_evaluated` | `via`(`meaning`, `resource`, `mediator`), `group_type`, `min_grade` | 의존하는 이중화 그룹에 도달하고, 그룹의 3가지 값을 계산할 입력(`min_alive`, 구성원)이 `min_grade` 이상으로 있어요 |
| `query_answers` | `query`(`impact`, `paths`, `exposure`), 질의별 필드, `watch_types` | 그 객체에 대한 응답에 `unknown`이 없어요(`watch_types`가 있으면 그 타입의 객체만 봐요) |
| `all_of`, `any_of`, `at_least` | `items`, `n` | 모두 / 하나라도 / `n`개 이상 |

- **이어 붙이기**는 `then` 안에 `then`을 몇 겹이든 넣어요. 다른 질문을 이름으로 가리키는 종류는 없어서 순환이 안 생겨요.
- **분기가 여럿일 때**(`peers`): `all`, `any`, `at_least: n`. `distinct_at: <need_id>`는 분기들이 그 단계에서 도달하는 객체가 **서로 달라야** 한다는 조건이에요. 서로 다른 객체에 도달하는 분기 `n`개의 조합이 있으면 충족한 거예요.
- 대상 하나의 평가는 읽은 객체와 읽은 항목(`aspect`)의 집합을 남겨요(6.3절).

### 4.2 등급과 2가지 읽기

패턴은 한 번 적고 **2가지 읽기**로 계산해요. `impact`의 2가지 응답과 같은 의미예요(ADR-0010).

| 읽기 | 입력을 이렇게 읽어요 |
|---|---|
| 사실만 | `assumed`인 값과 링크는 알 수 없음으로 읽어요. `fact`만 채워요 |
| 가정 포함 | `min_grade: assumed`(기본)인 필요는 `assumed`까지 채워요. `min_grade: fact`인 필요는 두 읽기 모두 `fact`만이에요 |

`unknown`은 어느 읽기에서도 채우지 못해요.

### 4.3 미충족 이유

| 이유 | 설명 | 해결 경로 |
|---|---|---|
| `absent` | 없다. 완전성 선언이 있는데 없어요 | 소스나 정의의 문제예요 |
| `unknown_absence` | 모른다. 선언이 없어 "없다"로 읽지 못해요 | 소스가 그 타입을 전부 주는지 선언해요(ADR-0007) |
| `grade_too_low` | 있는데 등급이 모자라요 | 사실로 올릴 새 관측이나 확인이 필요해요 |
| `too_stale` | 있는데 `max_age`보다 오래됐어요 | 피드를 복구해요 |
| `too_few`, `not_distinct` | `min_count`나 `n`보다 적어요 / 분기가 같은 객체로 모여요 | 설계가 이중이 아니에요 |
| `not_reached` | `follow`가 `max_steps` 안에 `until`에 도달하지 못했어요 | 체인이 끊겼어요 |
| `answer_unknown` | 사전 정의 질의의 응답에 `unknown`이 있어요 | 응답의 `unknown` 이유를 따라가요 |

### 4.4 대상을 선택하고 필터링하는 조건

`target.where`와 7절의 `filter`는 **같은 패턴**이에요. 위 종류에 값을 비교하는 `attribute_is`(`attribute`, `op`: `equals`, `in`, `prefix`, `range`, `value`)를 추가해 써요. 홉 수는 제한이 없어요: 운영 환경인 것만, 이 건물에 있는 것만, 이 비즈니스 서비스에서 `follow`로 도달하는 서버만. `attribute_is`는 선택과 필터링에만 써요. 값이 정해진 것을 요구하는 건 필요가 아니라 대상의 조건이에요.

### 4.5 한도

패턴의 깊이, 한 홉의 분기 수, 역방향 인덱스의 크기에 기본 한도가 있어요(13절, [기본값 표](defaults.md)). 한도를 넘는 패턴은 **저장할 때 경고해요.** 차단하지는 않아요. 평가 중에 넘으면 그 객체는 `not_evaluated`(이유: `depth_limit`, `fanout_limit`, `reads_limit`, `engine_pending`)로 따로 세어요.

### 4.6 가상의 예시

**예시 ① 이 서버는 A·B 두 전원에서 전기를 받는가.** 여러 홉과 `distinct`를 써요. 서버의 전원 장치 2개가 각각 콘센트와 랙 PDU를 거쳐 서로 다른 급전선에 도달해야 해요. 랙 PDU부터 급전선까지는 몇 홉인지 알 수 없어서 `follow`로 적어요.

```yaml
name: server_dual_power_chain
text: 이 서버는 A·B 두 전원에서 전기를 받는가
target: {object_type: PhysicalServer, where: {kind: attribute_is, attribute: operating_state, op: equals, value: in_service}}
need:
  kind: all_of
  items:
    - need_id: psu                      # 서버의 전원 장치
      kind: link_present
      link_type: PART_OF, direction: in, peer_type: PowerSupply
      min_count: 2, peers: {at_least: 2}, distinct_at: feed
      then:
        need_id: outlet                 # 전원 장치 -> PDU 콘센트
        kind: link_present
        link_type: POWERED_BY, direction: out, peer_type: PduOutlet
        then:
          need_id: pdu                  # 콘센트 -> 랙 PDU
          kind: link_present
          link_type: PART_OF, direction: out, peer_type: RackPdu
          then:
            need_id: feed               # 랙 PDU -> (몇 홉이든) -> 급전선
            kind: follow
            link_type: POWERED_BY, direction: out
            until: {peer_type: PowerFeed}
            max_steps: 6
    - need_id: power_group              # 서버가 의존하는 전력 이중화 그룹이 계산된다
      kind: group_evaluated
      via: {meaning: supplied_by, resource: power}
      group_type: PowerRedundancyGroup
      min_grade: assumed
tracked_groupings: [{grouping_id: by_site, group_by: {path: [MOUNTED_IN, LOCATED_IN], object_type: Site}}]
```

읽는 방법: `psu`의 도착(전원 장치)이 2개 이상이고, 각 전원 장치가 `outlet`, `pdu`, `feed`를 끝까지 충족해야 해요. 두 분기가 도달하는 급전선(`feed`)은 서로 달라야 해요(`distinct_at`). 같은 급전선에 도달하면 `not_distinct`예요. 마지막으로 서버가 의존하는 그룹이 계산돼야 `power_group`도 충족해요. 충족하지 못한 서버에는 어디서 끊겼는지가 남아요(예: `psu-7`의 `outlet`에서 `absent`).

**예시 ② ③ ④**(VM 페일오버, 스위치 영향 범위, 서비스 담당자)는 [역량 질문의 예시](competency-examples.md)에 패턴으로 끝까지 적었어요.

## 5. 충족률의 정의

**충족률 = 필요한 것을 다 채운 대상 객체의 수 ÷ 평가한 대상 객체의 수.**

- **대상 객체**: 지금 유효하고(폐기되지 않은) 대상 타입(하위 타입 포함)이며 `where`를 충족한 객체예요.
- **평가한 대상**: 대상에서 `not_evaluated`를 뺀 것이에요. `not_evaluated`의 수는 충족률 옆에 따로 보여요. 섞지 않아요.
- `where`를 따질 입력이 알 수 없음인 객체는 대상인지 알 수 없어요. 분모에 넣지 않고 `target_unknown`으로 따로 세요.
- 분모와 분자는 호출자가 읽을 수 있는 객체만 세요(**숨김 객체를 뺀 수**).
- 폐기된 객체는 분모에서 빠져요. 질의 엔진이 아직 계산하지 못하는 `capacity` 그룹에 의존하는 필요는 "못 채움"이 아니라 `engine_pending`인 `not_evaluated`예요. KG가 답하지 못하는 게 아니라 엔진이 아직 계산하지 못하는 거라서요.

### 5.1 객체별로 저장하는 것

derived 테이블 `competency_mark`예요(키는 `kg_id`, `question_id`, `object_id`). 원천 데이터에서 다시 만들 수 있어서 백업의 대상이 아니에요(ADR-0009). KG 버전의 ID와 내용 해시에 포함되지 않아요. **구조 질문**(`origin: structural`)은 타입마다 하나라 모든 객체가 대상이 돼서 표시가 객체 수만큼 쌓여요. 그래서 표시를 저장하지 않고 조회할 때 SQL로 집계하는 것이 기본이에요. 추적하기로 한 그룹(`tracked_groupings`)이 있는 질문은 그 그룹만 미리 계산해 둬요.

| 필드 | 설명 |
|---|---|
| `eval_state` | `evaluated`, `not_evaluated`(`not_evaluated_reason`) |
| `met_grade` | `fact`(사실만 읽기로 채웠어요), `assumed`(가정 포함 읽기로만 채웠어요), `none` |
| `unmet_fact`, `unmet_assumed` | 충족하지 못한 필요의 목록. 항목은 `need_id`, `reason`(4.3절), `step_path`(몇 번째 단계인지, 거쳐 온 `need_id`들), `cut_object_id`(끊긴 지점의 객체) |
| `evaluated_change_seq` | 어느 `change_seq`까지 반영했나 |
| `valid_until` | 시간이 지나기만 해도 판정이 바뀌는 가장 이른 시각(6.5절) |

`unmet_*`에는 패턴 순서(깊이 우선)로 **처음** 채우지 못한 단계가 앞에 오고, 다른 필요에서도 충족하지 못했으면 모두 남아요.

### 5.2 충족률 2종

`rate_fact`는 `met_grade`가 `fact`인 대상, `rate_with_assumptions`는 `fact`나 `assumed`인 대상이 분자예요. 둘의 차이(`assumed_only`)가 가정에 의존해서만 충족한 것이에요. 분자와 분모를 함께 남겨요.

### 5.3 이력으로 쌓이는 값

**속성으로 필터링한 충족률은 미리 쌓지 않아요.** 조회할 때 객체별 표시를 집계해 응답해요(7절). 쌓이는 것, 곧 KG 버전마다 남고 목표와 알림이 적용되는 것은 2가지뿐이에요.

1. 역량 질문 전체의 값.
2. 질문에 미리 적어 둔 그룹(`tracked_groupings`)의 값(사이트별, 환경별 등).

그 밖의 조합(예: 운영 환경이면서 이 건물인 것)의 **과거 이력**을 보려면 그 그룹을 질문에 적어야 해요(정의의 변경이라 제안을 거쳐요). 현재 값은 어떤 조합이든 조회할 수 있어요. 과거 이력은 적어 둔 뒤부터 쌓여요.

## 6. 자동 계산

### 6.1 실행 위치와 순서

**적용 트랜잭션 밖에서**, 변경 시퀀스를 따라가는 소비자(워커의 한 역할)가 실행돼요. 급전선 하나가 바뀌면 그것을 읽은 서버 수만 대를 다시 계산해야 하는데, 그것을 적용 경로에 얹으면 수집 스트림이 멈추기 때문이에요. 배치든 스트림이든 같은 경로예요(ADR-0008). 소비자는 변경 시퀀스와 `volatile`의 경량 변경 레코드를 함께 따라가요.

1. 변경 배치와 경량 변경 레코드에서 바뀐 객체와 링크, 속성을 모아요.
2. 역방향 인덱스(6.3절)로 영향받은 (질문, 대상 객체)를 찾아요. 같은 대상은 변경 배치 안에서 한 번만, 정해진 순서로 따져요.
3. 그 대상만 다시 계산하고 `competency_mark`를 갱신해요. 충족 수는 변경분만 가감해요.
4. 질문 전체와 `tracked_groupings`의 값을 지표로 발행해요(10.2절).

표시는 `evaluated_change_seq`를 달고 KG를 조금 늦게 따라가요(충족률의 답에 어느 `change_seq`까지 반영했는지가 붙어요). 같은 입력이면 같은 표시가 산출돼요. 처음부터 전부 계산한 결과와 증분 갱신한 결과가 같아야 하고, 같은 원본을 한 번에 넣은 것과 잘게 나눠 흘려 넣은 것도 같아야 해요. 테스트로 검증해요.

### 6.2 재계산을 일으키는 변경

| 바뀐 것 | 다시 계산하는 것 |
|---|---|
| 대상의 속성 | 그 속성을 읽은 대상 자신 |
| 링크의 추가·삭제·등급 변경 | 그 링크를 읽은 대상들. 새 링크도 "이 객체의 이 링크 타입을 읽었다"에 해당해요 |
| **몇 홉 떨어진** 객체나 링크 | 그 객체를 읽은 대상들(체인 전체가 읽은 집합에 있어서 한 번에 찾아요) |
| 그룹의 구성원, `min_alive` | 그 그룹을 읽은 대상들 |
| `volatile` 속성의 변경(경량 변경 레코드) | 그 속성을 읽은 대상 자신. 값이 같아 기록하지 않은 변경은 레코드가 없고, `seen_at`은 간격의 절반만큼 정밀도가 떨어져요(ADR-0009) |
| 객체의 추가·폐기 | 분모의 변화. 새 대상은 계산하고, 폐기된 대상은 표시를 삭제해요. 그 객체를 읽은 대상도 다시 계산해요 |
| 객체 병합 | 흡수된 객체의 표시는 삭제하고 남은 객체는 다시 계산해요. 흡수된 객체의 링크는 `identity_event`의 연결을 따라 읽은 항목을 옮기고 "링크가 사라지고 새로 생겼다"로 세지 않아요 |
| 완전성 선언의 변경 | 그 소스와 타입의 `absent`와 `unknown_absence` 판정 |
| 질문의 정의, 타입 등록부(`required`, 바인딩) | 그 질문의 모든 대상. 병합 전에 드라이 런으로 먼저 확인해요 |
| 시간 | `valid_until`에 도달한 것(6.5절) |

### 6.3 여러 홉에서 재계산 범위

대상 하나를 계산할 때 거쳐 간 객체마다 (`read_object_id`, `aspect`)를 남겨요. `aspect`는 속성 이름이거나 링크 타입과 방향이에요. 이 쌍을 역방향으로 찾는 인덱스가 `competency_reads`예요(`kg_id`, `read_object_id`, `aspect`, `question_id`, `object_id`. derived).

멀리 있는 급전선 하나가 바뀌면 `read_object_id`가 그 급전선인 행을 한 번 찾아 그 서버들만 다시 계산해요. 홉 수만큼 거슬러 올라가지 않아요. 같은 PDU를 읽은 서버가 많으면(팬아웃) 그만큼 다시 계산해요. 인덱스의 행 수는 대상 수 곱하기 읽은 객체 수라서 한도(`reads_limit`)가 있고, 넘으면 그 대상은 `not_evaluated`예요. 어림: 대상 100만에 읽은 객체 10개면 1천만 행이고, 3천만 객체의 구조 질문까지 저장하면 수억~수십억 행이 돼요(가정). 구조 질문의 표시를 저장하지 않는 이유예요. 합성 100만 객체에서 측정해서 [기본값 표](defaults.md)에 적어요.

### 6.4 사전 정의 질의에 의존하는 필요

`query_answers`는 KG 전체를 가로지르는 계산이라 소비자가 아니라 **질의 엔진이** 프로젝션의 변경을 따라 다시 계산하고 **워커가 그 결과를 수신해 기록해요.** **알파에서는 주기적으로 다시 계산해요**(주기의 값은 기본값 표에 가정으로 둬요). 변경이 영향을 준 영역의 대상만 골라 다시 계산하는 증분 갱신은 클로즈드 베타예요. 어떤 대상의 영향 범위가 그 변경으로 달라지는지를 아는 일이 곧 영향 범위 계산이라서 엔진이 있어야 해요. 적용 트랜잭션과 묶지 않아서 표시가 조금 늦게 따라오고, 표시마다 `evaluated_change_seq`가 있어요.

### 6.5 신선도는 시간이 지나기만 해도 바뀌어요

`max_age`가 있는 필요는 아무 변경이 없어도 어느 시점에 못 채움으로 바뀌어요. 그래서 평가할 때마다 `valid_until`(읽은 값의 관측 시각 더하기 `max_age` 중 가장 이른 시각)을 표시에 남기고 마감 인덱스에 넣어요.

- 시계도 이벤트예요. 정해진 간격(기본값(설정) 1분)의 **시계 이벤트**가 변경 시퀀스에 들어가고(ADR-0009), 그 이벤트에서 마감이 지난 표시만 다시 계산해요. 다시 실행해도 같은 결과가 나와요.
- 값을 다시 관측한 시각(`seen_at`)이 갱신됐다고 매번 수정하지 않아요. 마감이 됐을 때 현재 관측 시각을 읽어, 아직 신선하면 새 마감만 남겨요.
- 신선도의 근거는 속성별 신선도(ADR-0008)이고, 관측으로 읽는 링크는 관측의 경과 시간이에요.

### 6.6 드라이 런

제안의 드라이 런은 같은 계산을 후보에서 실행해요. 차이만 산출하는 드라이 런은 후보와 현재 버전이 다른 객체의 영향받은 대상만 다시 계산하고, 통째로 빌드한 후보 KG는 전부 계산해요. 결과는 10.1절의 "이전과 이후"로 산출돼요.

## 7. 질의 `competency`

사전 정의 질의에 `competency`를 추가해요(13번째. 읽기 전용이고 REST API와 MCP가 같은 유스케이스 위에서 제공해요). 공통 입력과 엔벨로프는 [KG 질의](kg-queries.md)를 따라요. 엔벨로프에 신선도와 `health_hint`가 붙어요.

| 입력 필드 | 설명 |
|---|---|
| `question_ids`, `origins`, `layers`, `object_types` | 대상 질문(없으면 목록) |
| `object_ids` | 이 객체들에 대해 답할 수 있는 것을 조회한다(객체 모드) |
| `filter` | 대상을 필터링하는 패턴(4.4절). 홉 수는 제한이 없어요 |
| `group_by` | 그룹 키: `attribute`, `object_type`, `layer`, 링크를 따라간 객체의 속성이나 이름(`path`) |
| `mode` | `both`(기본), `facts_only`, `with_assumptions` |
| `breakdown` | `none`, `by_step`, `by_need`, `by_source`, `by_grade` |
| `history` | KG 버전별 과거 값. 질문 전체나 `tracked_groupings`의 그룹만 가능해요. 다른 `filter`나 `group_by`와 함께 쓰면 거절해요(5.3절) |
| `include` | `rates`, `unmet_objects`, `gaps`(10.3절), `needs` |

출력: `questions[]`(`question_id`, `name`, `text`, `denominator`, `not_evaluated`, `target_unknown`, `numerator_fact`, `numerator_with_assumptions`, `rate_fact`, `rate_with_assumptions`, `objective_state`, `population`(12절)), `groups[]`(그룹 키별로 같은 수치들), `breakdown`(8절), `items`(충족하지 못한 객체: `object_id`, `display_name`, `unmet`. 페이지네이션), 객체 모드는 객체별로 질문마다 `met_grade`와 `unmet`이에요.

- **필터링한 값은 조회할 때 집계해요.** `filter`와 `group_by`의 값은 `competency_mark`를 객체의 속성과 조인해 집계해서 즉시 응답해요(5.3절). SQL로 충분한지 엔진이 필요한지는 측정해서 가려요.
- 숨김 속성으로 필터링하거나 그룹화하면 거절해요. 읽을 수 없는 객체는 목록에 안 나오고, 분모와 분자와 그룹의 수는 **숨김 객체를 뺀 수**예요. 숨김 객체는 범위가 전체가 아닌 주체에게 정확한 수 대신 있음/없음만 제공해요(설정으로 수를 열 수 있어요. ADR-0012).

## 8. 필터링, 그룹화, 세부 분석

### 8.1 여러 홉을 따라간 속성으로 필터링

예: "점검 중이 아닌 호스트에서 실행되는 VM만"은 `RUNS_ON` 1홉, "이 클러스터의 호스트에서 실행되는 VM만"은 2홉(`RUNS_ON`, `MEMBER_OF`)이에요. 그룹 키도 `path`로 링크를 따라간 객체를 써요(예: `OWNED_BY` 1홉의 `Team`).

### 8.2 가상의 결과 예시

질문 `server_dual_power_chain`을 사이트별로 그룹화해요(`group_by`: `MOUNTED_IN`, `LOCATED_IN`을 따라간 `Site`).

| 사이트 | 대상 | 사실만 채움 | 가정 포함 채움 | `rate_fact` | `rate_with_assumptions` | `not_evaluated` | 모집단 |
|---|---|---|---|---|---|---|---|
| `site-a` | 120 | 96 | 108 | 80.0% | 90.0% | 0 | 선언됨 |
| `site-b` | 80 | 40 | 52 | 50.0% | 65.0% | 0 | 선언됨, 3대 부족 |
| `site-c` | 30 | 30 | 30 | 100.0% | 100.0% | 0 | **선언 없음** |

`site-c`의 100%는 주의해서 읽어야 해요(12절). 이 그룹은 질문에 `by_site`로 적어 뒀으니 과거 이력도 쌓여요. 환경별(`service_has_owner`)은 같은 형식의 표로 `production` 85.0%(사실만)와 90.0%(가정 포함), `staging` 50.0%와 55.0%, `development` 25.0%와 25.0%처럼 읽어요.

### 8.3 단계별 충족률

`breakdown: by_step`이에요. 사실만 읽기로 `server_dual_power_chain`을 계산한 가상의 결과예요(대상 203, `not_evaluated` 3, 평가 200). 단계는 패턴 순서이고, 각 행은 그 단계까지 충족한 대상이에요.

| 단계(`need_id`) | 거기까지 충족 | 충족률 | 처음 끊긴 수 | 주된 이유 |
|---|---|---|---|---|
| `psu` 전원 장치 2개 이상 | 196 | 98.0% | 4 | `too_few` |
| `outlet` 전원 장치 -> 콘센트 | 122 | 61.0% | 74 | `unknown_absence` 70, `absent` 4 |
| `pdu` 콘센트 -> 랙 PDU | 120 | 60.0% | 2 | `absent` |
| `feed` 랙 PDU -> 급전선(`follow`) | 118 | 59.0% | 2 | `not_reached` |
| `psu`의 `distinct_at` | 112 | 56.0% | 6 | `not_distinct` |
| `power_group` | 110 | 55.0% | 2 | `grade_too_low` |

가장 많이 끊기는 곳은 전원 장치에서 콘센트로 가는 단계이고 대부분 "알 수 없음"이에요. 그 정보를 제공하는 소스를 연결하면 올라요.

### 8.4 미충족 원인: 필요, 소스, 피드

- **어느 필요에서**: `by_need`는 필요별로 충족하지 못한 대상의 수와 이유별 분포, 그 필요 하나만 부족한 대상의 수(`blocking_alone`)예요. `by_grade`는 `assumed_only`를 보여요.
- **어느 소스와 피드**: 필요가 읽는 타입, 속성, 링크 타입별로 정의의 계보(ADR-0014)를 거슬러 올라가 그 데이터를 제공하는 피드와 소스를 찾고, 충족하지 못한 대상이 어느 피드가 만든 객체인지(원본 레코드의 참조)를 집계해요. 분류는 4가지예요: `no_supplier`(제공하는 피드가 없어요. 소스를 더 연결해야 해요) / `supplier_gaps`(피드는 있는데 값이 비어 있거나 연결하지 못한 링크가 문제 목록에 있어요) / `supplier_unhealthy`(피드나 소스의 헬스 상태가 비정상이에요. `affected_upstream`) / `grade_only`(값은 있지만 등급이 부족해요).

## 9. 역량 질문이 생기는 경로 4가지

| 경로 | 주체 | 형태 |
|---|---|---|
| `standard` | 표준 모델에 포함된다 | 층별 기본 질문. 표준 모델과 함께 버전이 올라요 |
| `structural` | 타입 등록부에서 자동으로 생성된다 | 타입마다 하나. `required`인 속성은 `attribute_present`, 최소 개수가 있는 바인딩은 `link_present`로 이은 `all_of`예요. 하위 타입은 부모의 것을 상속해요. 저장하지 않고 등록부에서 도출해요 |
| `user` | `editor`가 적는다 | 정의의 제안 절차를 거쳐요. API가 정의를 수신하고, 편집 화면은 프런트엔드 리포지토리의 범위예요 |
| `suggested` | 사용 이력에서 제안된다 | `unknown`이나 빈 응답으로 끝난 질문이 반복되면(ADR-0013의 사용 이력) 후보로 올라와요. 누군가 채택해서 기록하면 `user`와 같은 경로예요. 반복 기준은 알파에서 정해요. 구현은 클로즈드 베타이고 알파에는 계약에 플레이스홀더만 있어요 |

### 9.1 표준 모델의 기본 질문(층마다 2~3개의 예)

| 층 | 질문 | 대상 | 필요한 것 |
|---|---|---|---|
| 위치와 설비 | 이 랙은 어느 방의 어느 전원에서 전기를 받나 | `Rack` | `LOCATED_IN`으로 `Room`, `follow`로 `PowerFeed` |
| | 이 방은 어느 냉방 설비가 식히나 | `Room` | `supplied_by(cooling)`의 설비, 그 설비의 전원 |
| 물리 서버와 부품 | 이 서버는 어느 랙의 어느 위치에 있나 | `PhysicalServer` | `MOUNTED_IN`의 `Rack`, 위치 속성 |
| | 이 서버는 A·B 두 전원에서 전기를 받나 | `PhysicalServer` | 예시 ① |
| 네트워크 | 이 스위치에 장애가 나면 어느 서비스가 영향받나 | `Switch` | 예시 ③ |
| | 이 스위치는 이중화 쌍이나 그룹을 아나 | `Switch` | `group_evaluated`(`member_of`) |
| 스토리지 | 이 볼륨은 어느 스토리지 시스템이 제공하나 | `StorageVolume` | `supplied_by(storage)`를 `follow`해 `StorageArray` |
| | 이 볼륨은 어느 호스트에 노출되나 | `StorageVolume` | `EXPORTED_TO` 1홉 이상 |
| 가상화 | 이 VM은 어느 호스트에서 실행되나 | `VirtualMachine` | `RUNS_ON`의 호스트, `follow`로 `PhysicalServer` |
| | 이 VM이 실행되는 호스트에 장애가 나면 어디로 페일오버되나 | `VirtualMachine` | 예시 ② |
| 컨테이너 | 이 파드는 어느 노드와 VM과 서버 위에서 실행되나 | `Pod` | `RUNS_ON`을 `follow`해 `PhysicalServer`까지 |
| | 이 워크로드는 어느 서비스를 실현하나 | `Workload` | `REALIZED_BY`의 역방향 1홉 이상 |
| 플랫폼과 OS | 이 OS는 어느 서버나 VM 위인가 | `OperatingSystem` | `RUNS_ON`의 `VirtualMachine`이나 `PhysicalServer` |
| | 이 소프트웨어는 어느 OS 위에서 실행되나 | `SoftwareInstance` | `RUNS_ON`의 `OperatingSystem` |
| 서비스와 조직 | 이 서비스는 누가 담당하나 | `ApplicationService` | 예시 ④ |
| | 이 서비스는 어느 설비까지 이어지나 | `BusinessService` | `REALIZED_BY`를 `follow`해 `PhysicalServer`, 거기서 `PowerFeed`까지 |
| 공통 | 이 이중화 그룹은 계산되나 | `RedundancyGroup` | `min_alive`(또는 용량), 구성원 2개 이상 |
| | 이 장애 영역에는 무엇이 포함돼 있나 | `FailureDomain` | `contained_in` 1홉 이상 |

### 9.2 사람이 작성하되 빈 상태에서 시작하지 않아요
패턴은 사람이 작성하고 방법은 3가지예요(REQ-195, ADR-0015). (1) 표준 모델의 질문과 틀을 복사해 수정해요. (2) **예시 객체에서 생성해요**: 답이 되는 객체 하나를 고르면 제품이 그 객체에서 도달 경로(전원 체인, 호스트, 담당 조직)를 읽기 권한 범위 안에서 보여 주고, 고른 경로가 패턴의 뼈대가 돼요. (3) 사용 이력에서 제안해요(`suggested`). 자연어로 적은 질문을 언어 모델로 패턴에 변환하는 일은 제품에 넣지 않아요. 같은 입력에 같은 결과가 나오지 않고 제품이 언어 모델에 의존하게 돼서예요. 외부 에이전트가 API로 하면 되고, 패턴은 고정된 문법이라 저장할 때 검증돼요.

## 10. 활용

### 10.1 드라이 런의 검사

제안 하나를 드라이 런하면 질문별로 이렇게 보여요(가상의 예).

| 질문 | 이전 `rate_fact` | 이후 `rate_fact` | 변화 | 새로 못 채움 | 새로 채움 | 목표 |
|---|---|---|---|---|---|---|
| `server_dual_power_chain` | 55.0% | 37.5% | -17.5점 | 38 | 3 | 50.0% 아래로 떨어짐 |
| `service_has_owner` | 85.0% | 85.0% | 0점 | 0 | 0 | 지킴 |

이전에는 목표를 지켰는데 이후에는 못 지키면 **차단 사유**가 돼요(`competency_objective_missed`). 새로 못 채운 객체의 목록이 보여서 정의의 어디를 수정했는지 짚을 수 있어요. 이미 목표 밑인 질문을 더 낮추는 제안은 기본으로 차단하지 않고 **경고**해요. 질문의 `block_on_regression`을 켜면 "지금보다 낮추는 제안"도 차단 사유가 돼요.

### 10.2 관측, 목표, 헬스 상태, 알림

관측성 문서(ADR-0013)가 충족률을 지표 `competency_fill_rate`(필요한 것을 다 채운 대상 ÷ 대상, 분자와 분모를 함께 남겨요)와 `competency_not_evaluated`(한도나 엔진 때문에 계산하지 못한 객체 수)로 수집해요. `metric_point`에는 `step_key`(몇 번째 단계)와 `group_key`(그룹의 값) 필드를 둬요. 이 지표는 **질문 전체와 `tracked_groupings`의 그룹**에만 KG 버전마다 쌓여요(5.3절). 목표(`objective`, 목표의 종류 `competency_fill`)와 알림도 이 값들에만 적용돼요. 판정은 ADR-0013의 목표(지킴, 못 지킴, 알 수 없음, 못 지킨 비율)와 같은 틀이고 헬스 상태와 알림으로 전달돼요. 못 채운 이유가 이상 상태인 소스에 있으면(8.4절의 `supplier_unhealthy`) 알림은 소스의 근본 원인 하나로 묶여요. 표시가 KG를 따라오는 지연(`evaluation_lag`)도 지표예요.

### 10.3 공백 보고: 무엇부터 연결할까

`include: gaps`는 필요별로 `blocking_alone`(이 필요 하나만 채우면 충족되는 대상 수), 분류(8.4절), 그리고 `no_supplier`이면 그 속성이나 링크를 기본 매핑으로 채우는 프리셋의 목록을 반환해요. 정렬은 `blocking_alone`이 큰 순서예요. 그 수는 **올릴 수 있는 상한**이에요. 값이 들어와도 `unknown`이나 `assumed`일 수 있어서요.

### 10.4 에이전트와 시스템이 조회하는 경로

MCP의 `competency` 툴이에요. 읽기만 해요. 에이전트가 `impact`를 쓰기 전에 "이 스위치에 대해 이 KG가 무엇에 답할 수 있나"를 먼저 조회해요.

```yaml
# 입력: {object_ids: [sw-12], include: [needs], detail: brief}
# 출력의 data (엔벨로프는 kg-queries의 4절을 따른다)
objects: [{object_id: sw-12, questions: [{name: switch_impact_answerable, met_grade: none,
  unmet_fact: [{need_id: impact_ok, reason: answer_unknown, cut_object_id: svc-9}]}]}]
```

`impact`의 응답에 `unknown`이 섞일 것이고 그 이유가 `svc-9`라는 걸 에이전트가 알아요. 응답이 크면 요약을 먼저 반환하고 `detail`로 상세 보기를 선택해요.

## 11. 알파의 범위

화면은 다른 리포지토리의 범위예요(ADR-0016). 이 리포지토리가 제공하는 것은 API(역량 질문, 충족률의 필터링과 그룹화, 단계별 충족률, 미충족 객체, 공백 보고)이고, 알파의 기준은 **API와 CLI로 모두 할 수 있다**예요. 정의는 API로 수정하고 브랜치와 제안을 거쳐요.

## 12. 함정과 알려진 한계

- **모집단이 덜 들어오면 충족률이 높게 나와요.** 분모는 KG에 **들어온** 대상이에요. 소스가 서버를 일부만 제공했으면 들어온 것만 계산해서 비율이 좋아 보여요. 그래서 충족률 옆에 `population`을 붙여요: `declared`(대상 타입의 완전성 선언이 있어요), `short`(선언한 수와 수집한 수가 달라 문제 목록에 올라 있어요), `undeclared`(선언이 없어요). 완전성 선언에는 선택적인 기대 수(`expected_count`와 그 근거)를 둘 수 있고, 있으면 "대상이 얼마나 들어왔나"(들어온 수 ÷ 기대 수, `arrival_ratio`)를 충족률 옆에 함께 보여요. 없으면 표시만 해요. `short`와 `undeclared`는 충족률을 깨끗하다고 읽으면 안 된다는 표시예요. 8.2절의 `site-c`가 그래요.
- **"없다"와 "모른다"**: 선언이 없으면 링크가 없어도 `unknown_absence`예요. 둘 다 못 채움이지만 해결 경로가 달라서(선언 대 소스) 따로 남겨요. 어느 쪽도 "채웠다"가 되지 않아요.
- **충족했다는 것이 값이 맞다는 의미는 아니에요.** 값이 있고 등급이 조건에 맞다는 의미예요. 소스가 틀린 값을 제공해도 충족해요.
- **계산이 무거워질 수 있어요.** 패턴이 깊고 분기가 많으면, 허브(많은 대상이 읽는 PDU 하나)가 바뀔 때 다시 계산할 대상이 갑자기 늘어요. 한도(4.5절)가 있고, 한도에 걸린 것은 `not_evaluated`로 드러나요. 한도를 수정하면 분모가 바뀌므로 정의의 변경으로 드라이 런을 거쳐요.
- **`distinct_at`은 같은 단계의 객체만 비교해요.** 서로 다른 급전선이 실제로 다른 정전 영역인지는 말하지 않아요. 그건 `impact`가 다루는 범위라 `query_answers`를 함께 적용해요.
- 충족률은 구성(정의된 체인)에 대한 것이에요. 현재 값이나 헬스 상태는 ADR-0013이 다뤄요. 시간(지속 시간 등)은 계산하지 않아요. 질의 엔진이 `capacity` 그룹을 구현하기 전까지 그 그룹에 의존하는 필요는 `not_evaluated`로 세요. `query_answers`의 표시는 알파에서 주기적으로 다시 계산하므로 변경보다 늦어요(6.4절).
- 질문 사이의 참조가 없어서 한 질문이 다른 질문의 충족을 필요로 할 수 없어요.

## 13. 숫자는 가정이에요

| 값 | 기본값 | 측정해서 정할 곳 |
|---|---|---|
| 패턴의 깊이 한도 | 8 | 합성 3천만 객체에서 평가 시간 |
| 한 홉의 분기 수 한도 | 1천 | 허브(PDU, 스위치)의 팬아웃 |
| `follow`의 `max_steps` 상한 | 12 | 실제 체인의 길이 |
| 읽은 항목의 인덱스(`reads_limit`) | 대상 하나당 500 | 인덱스의 행 수(합성 100만 객체에서) |
| 시계 이벤트의 간격 | 1분 | 마감과 부하 |
| `query_answers`를 다시 계산하는 주기(알파) | 아직 없음(가정으로 정해요) | 엔진의 영향 범위 계산 시간 |
| 질문 하나의 노드 수 상한 | 30 | 정의의 검사 시간 |

## 14. 미결 사항

- 스트림 수집에서 작은 변경 배치마다 계산할지, 모아서 계산할지. 표시가 얼마나 지연돼도 되는지.
- 구조 질문에 `tracked_groupings`가 있을 때 객체별 표시를 저장하는지, 그룹의 수만 쌓는지.
- 사용 이력에서 제안하는 기준(반복 횟수, 기간), 표준 모델의 기본 질문을 조직이 끄거나 수정하는 경로.
- 필터링 값을 SQL로 집계하는 속도, 엔진으로 넘길 시점.

## 15. 관련 결정과 요구사항

- [ADR-0015](adr/0015-competency-questions.md): 이 문서의 결정.
- ADR-0007([KG의 모델](kg-model.md)): `required`, 근거 등급, 완전성 선언. ADR-0008: 피드, 속성별 신선도. ADR-0010([KG 질의](kg-queries.md)): 사전 정의 질의와 엔벨로프, 영향 범위의 두 답. ADR-0013([데이터 관측성](data-observability.md)): 지표, 목표, 헬스 상태, 알림. ADR-0014: 정의, 드라이 런, 계보. ADR-0016: 화면은 다른 리포지토리.
- 요구사항: REQ-190, REQ-191, REQ-192, REQ-193, REQ-194, REQ-195.
