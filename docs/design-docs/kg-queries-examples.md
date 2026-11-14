---
title: 영향 범위를 손으로 따라가기 — 가상의 예시 5개
kind: guide
status: active
follows:
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
---
# 영향 범위를 손으로 따라가기

[KG 질의](kg-queries.md)의 5절(영향 범위의 의미)을 가상의 구성으로 끝까지 따라가 보는 예시 모음이에요. 서버, 포트, 숫자는 모두 가상이에요. 그룹의 값은 `A`(정상), `D`(장애), `U`(알 수 없음)로 적어요. 결과 이름은 5.4절을 따라요. 노출은 끝까지 전파되므로 자기의 이중화를 잃은 객체는 `exposed_direct`, 그 위에 의존하는 객체는 `exposed_inherited`예요.

## 1. A·B 전원 중 PDU 하나에 장애

`srv-1`, `srv-2`, `srv-3`은 전원 피드 장치(`PowerSupply`)를 2개씩 가지고, 각 쌍은 `min_alive = 1`인 그룹 `g-pwr-N`에 속해요. 서버는 그룹에 `supplied_by`(power)로 의존해요. `vm-1`은 `srv-1`에 `runs_on`으로 올라가 있어요. `dead = [pdu-a]`.

| 서버 | 피드 장치가 의존하는 PDU | 추적한 값 | 그룹 | 결과 |
|---|---|---|---|---|
| `srv-1` | `psu-1a`→`pdu-a`, `psu-1b`→`pdu-b` (`fact`) | 1a 장애, 1b 정상 | `a=1 >= 1`: A, `slack=0` | `exposed_direct` |
| `vm-1` | `srv-1`에 `runs_on` | 의존하는 `srv-1`이 노출됨 | | `exposed_inherited` |
| `srv-2` | `psu-2a`→`pdu-a`, `psu-2b`→`pdu-a` (배선이 한 PDU에 몰려 있음. `fact`) | 둘 다 장애 | `a+u=0 < 1`: D | `down`, `lost_channels: [power]` |
| `srv-3` | `psu-3a`→`pdu-a` (`fact`), `psu-3b`→`pdu-b` (**`assumed`**) | 3a 장애, 3b는 사실만 반영한 응답에서 알 수 없음, 가정 포함 응답에서 정상 | 사실만 반영: `a=0,u=1`: U. 가정: `a=1`: A | `unknown` / `exposed_direct` |

`exposure`는 `g-pwr-1`의 `slack`이 0이라 후보로 `psu-1b`와 그 상위의 `pdu-b`를 산출해요. 겉으로만 이중화인 `srv-2`는 공통 원인을 추적해서 찾았어요. `vm-1`이 노출로 나오는 것은 `srv-1`의 노출을 상속받아서예요. 상속된 노출의 원인을 거슬러 올라가면 언제나 직접 노출이 나와요.

## 2. LAG의 구성원 하나에 장애

`lag-7`(`Lag`, 방식 `count`)은 포트 `port-1`~`port-4`가 구성원이고 `min_alive = 2`(장비 설정의 최소 링크 수. 설정에서 수집할 때 만든 `configured` 그룹이고 `fact`)예요. `srv-9`가 `lag-7`에 `supplied_by`(network)로 의존해요. 범위에 앵커 `core-1`(`NetworkAnchor`)이 있고 `srv-9`는 살아 있는 간선으로 도달해요(앵커가 없으면 `unknown`). 포트를 스위치에 어떤 의미로 연결하는지는 ADR-0007이 정해요. 여기서는 포트 자체에 장애가 난 경우만 보고, `view`는 기본인 `as_observed`예요. 이웃 테이블 같은 관측 그룹이 따로 있으면 그것을 사용하고 엔벨로프의 `evidence.kinds`에 `observed`가 적혀요.

| `dead` | `a` | `lag-7` | `srv-9` |
|---|---|---|---|
| `port-3` | 3 | A, `slack=1` | `degraded` |
| `port-3`, `port-4` | 2 | A, `slack=0` (`exposure` 후보 `port-1`, `port-2`) | `exposed_direct` |
| `port-2`, `port-3`, `port-4` | 1 (`1+0 < 2`) | D | `down`, `lost_channels: [network]` |
| 위와 같고 `min_alive`를 1로 낮춤 | 1 (`1 >= 1`) | A, `slack=0` | `exposed_direct` |

## 3. HA 클러스터의 호스트 하나에 장애

`ha-1`(`HaCluster`)은 호스트 `h1`, `h2`, `h3`이 구성원이고 `min_alive = 2`예요. `vm-a`는 `h1`에, `vm-b`는 `h2`에 `runs_on`으로 올라가 있고 둘 다 `failover_to`가 `ha-1`이에요.

| `dead` | `ha-1` | `vm-a` | `vm-b` |
|---|---|---|---|
| `h1` | `a=2`: A, `slack=0` | `interrupted` (호스트는 장애지만 인계받을 그룹이 정상이다) | `exposed_direct` |
| `h1`, `h2` | `a=1 < 2`: D | `down`, `lost_channels: [service]` | `down` |
| `h1`, `min_alive`를 3으로 높임 | `a=2 < 3`: D | `down` | `h2`가 정상이라 `down`이 아니다(인계받을 그룹만 장애다) |
| `h1`, `min_alive`가 `assumed` (사실만 반영한 응답) | 1, 2면 A, 3이면 D라 U | `unknown` (그룹의 여유를 알 수 없다) | `unknown` |

첫 행에서 `ha-1`은 여유가 0이라 `exposure`가 `h2`, `h3`을 후보로 산출해요. 호스트 장애 후 VM이 재기동되는 사이의 중단과 남은 호스트의 용량은 고려하지 않아요. `model_limits`에 안내 문구가 붙어요.

## 4. MLAG 쌍의 스위치 하나에 장애

`sw-a`, `sw-b`는 `mlag-1`(`min_alive = 1`)의 구성원이에요. `vsw-1`(논리 스위치)은 `mlag-1`에 `realized_by`로, `srv-1`은 `mlag-1`에 `supplied_by`(network)로 의존해요. `core-1`은 앵커예요. 연결성 간선: `core-1`-`sw-a`, `core-1`-`sw-b`, `sw-a`-`sw-b`, `srv-1`-`sw-a`, `srv-1`-`sw-b`.

**(가) `dead = [sw-a]`.** 의존: `mlag-1`의 `a=1 >= 1`이라 A, `slack=0`. 연결성: 정상 간선은 `core-1`-`sw-b`, `sw-b`-`srv-1`이고 `sw-b`, `srv-1`에 도달해요. 결과: `vsw-1`과 `srv-1`은 `exposed_direct`, 손실된 채널은 없어요. `exposure`는 `sw-b`를 후보로 산출해요.

**(나) `dead_links = [core-1-sw-a, core-1-sw-b]`**(코어 쪽 업링크 2개).

| 계산 | 결과 |
|---|---|
| 의존만 (장애 객체가 없으므로) | `sw-a`, `sw-b` 정상, `mlag-1` A. 모두 정상 |
| 개수 계산만 (서로를 정상 이웃으로 집계) | `sw-a`는 `sw-b`가, `sw-b`는 `sw-a`가 정상 이웃이라 둘 다 정상. 같은 오답 |
| 연결성 (`core-1`에서 출발) | `core-1`에 도달하는 정상 간선이 없다. `sw-a`, `sw-b`, `srv-1` 모두 `no` |
| 교대 반복 | 도달하지 못한 `sw-a`, `sw-b`가 `network`를 잃어 `down`. `mlag-1`의 `a=0`: D. `vsw-1`은 `down`(`lost_channels: [service]`), `srv-1`은 `down`(`lost_channels: [network]`) |

(나)에서 의존만 사용하면 낙관적으로 틀린 결과가 나와요. 이것이 두 계산을 분리하는 이유이고, kg-queries.md 7절의 테스트가 이런 형태의 입력으로 검증해요.

## 5. N+1 냉방 그룹에서 하나에 장애

`cool-grp-1`은 방식 `count`(`n_plus_1`)인 `RedundancyGroup`이고 구성원은 `crah-1`~`crah-3`, `min_alive = 2`예요. 홀의 서버 `srv-21`~`srv-23`은 `cool-grp-1`에 `supplied_by`(cooling)로 의존하고, 이 링크의 `delay_class`는 `delayed`예요. 각 서버 위에서 VM이 하나씩 실행돼요(`runs_on`).

| `dead` | `a` | `cool-grp-1` | 서버 3대 | 그 위의 VM |
|---|---|---|---|---|
| `crah-2` | 2 | A, `slack=0` (`exposure` 후보 `crah-1`, `crah-3`) | `exposed_direct` | `exposed_inherited` |
| `crah-2`, `crah-3` | 1 | D | `down`, `lost_channels: [cooling]`, `delay_class: delayed`(지연되어 나타남) | `down` |

`exposure`는 후보마다 `would_die_count = 3`을 산출해요. 이제 `min_alive = 2`가 **`assumed`**(냉방 용량을 소스가 제공하지 않아 관리자가 입력한 값)라고 해 봐요. 사실만 반영한 응답에서 `min_alive`는 알 수 없음이라 1, 2, 3을 모두 따져요. `dead = [crah-2]`에서는 `a=2`, `n=3`이에요. `min_alive`가 1이나 2면 A, 3이면 `a+u < 3`이라 D이므로 결과가 갈려서 **알 수 없음**이에요. 가정 포함 응답은 A(`slack=0`)라 서버는 `exposed_direct`, VM은 `exposed_inherited`예요. 그래서 서버 3대는 사실만 반영한 응답에서 `unknown`, 가정 포함 응답에서 `exposed_direct`예요.
