---
title: 영향 범위를 손으로 따라가기 — 지어낸 예시 다섯
kind: guide
status: active
follows:
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
---
# 영향 범위를 손으로 따라가기

[KG에 묻기](kg-queries.md)의 5절(영향 범위의 뜻)을 지어낸 회로로 끝까지 따라가 보는 예시 모음이에요. 서버, 포트, 숫자는 모두 지어낸 것이에요. 묶음의 값은 `A`(산), `D`(죽은), `U`(모름)로 적어요. 결과 이름은 5.4절을 따라요. 노출됨은 끝까지 번지므로 자기의 이중화를 잃은 객체는 `exposed_direct`, 그 위에 기대는 객체는 `exposed_inherited`예요.

## 1. A·B 전원 중 PDU 하나가 죽음

`srv-1`, `srv-2`, `srv-3`은 전원 공급 장치(`PowerSupply`)를 둘씩 가지고, 각 쌍은 `min_alive = 1`인 묶음 `g-pwr-N`에 들어요. 서버는 묶음에 `supplied_by`(power)로 기대요. `vm-1`은 `srv-1`에 `runs_on`으로 얹혀 있어요. `dead = [pdu-a]`.

| 서버 | 공급 장치가 기대는 PDU | 따라간 값 | 묶음 | 결과 |
|---|---|---|---|---|
| `srv-1` | `psu-1a`→`pdu-a`, `psu-1b`→`pdu-b` (`fact`) | 1a 죽음, 1b 삶 | `a=1 >= 1`: A, `slack=0` | `exposed_direct` |
| `vm-1` | `srv-1`에 `runs_on` | 기대는 `srv-1`이 노출됨 | | `exposed_inherited` |
| `srv-2` | `psu-2a`→`pdu-a`, `psu-2b`→`pdu-a` (배선이 한 PDU에 몰림. `fact`) | 둘 다 죽음 | `a+u=0 < 1`: D | `down`, `lost_channels: [power]` |
| `srv-3` | `psu-3a`→`pdu-a` (`fact`), `psu-3b`→`pdu-b` (**`assumed`**) | 3a 죽음, 3b는 사실만 쓴 답에서 모름, 가정을 넣은 답에서 삶 | 사실만: `a=0,u=1`: U. 가정: `a=1`: A | `unknown` / `exposed_direct` |

`exposure`는 `g-pwr-1`의 `slack`이 0이라 후보로 `psu-1b`와 그 위쪽 `pdu-b`를 내요. 겉으로만 이중화인 `srv-2`는 공통 원인을 따라가서 찾았어요. `vm-1`이 노출됨으로 나오는 것은 `srv-1`의 노출됨을 물려받아서예요. 물려받은 노출됨의 뿌리를 거슬러 가면 언제나 직접 노출됨이 나와요.

## 2. LAG의 구성원 하나가 죽음

`lag-7`(`Lag`, 방식 `count`)은 포트 `port-1`~`port-4`가 구성원이고 `min_alive = 2`(장비 설정의 최소 링크 수, 설정에서 받을 때 만든 `configured` 묶음이고 `fact`)예요. `srv-9`가 `lag-7`에 `supplied_by`(network)로 기대요. 범위에 닻 `core-1`(`NetworkAnchor`)이 있고 `srv-9`는 살아 있는 간선으로 닿아요(닻이 없으면 `unknown`). 포트를 스위치에 어떤 뜻으로 잇는지는 ADR-0007이 정해요. 여기서는 포트 자체가 죽는 경우만 보고, `view`는 기본인 `as_observed`예요. 이웃 표 같은 관측 묶음이 따로 있으면 그것을 쓰고 봉투의 `evidence.kinds`에 `observed`가 적혀요.

| `dead` | `a` | `lag-7` | `srv-9` |
|---|---|---|---|
| `port-3` | 3 | A, `slack=1` | `degraded` |
| `port-3`, `port-4` | 2 | A, `slack=0` (`exposure` 후보 `port-1`, `port-2`) | `exposed_direct` |
| `port-2`, `port-3`, `port-4` | 1 (`1+0 < 2`) | D | `down`, `lost_channels: [network]` |
| 위와 같고 `min_alive`를 1로 낮춤 | 1 (`1 >= 1`) | A, `slack=0` | `exposed_direct` |

## 3. HA 클러스터의 호스트 하나가 죽음

`ha-1`(`HaCluster`)은 호스트 `h1`, `h2`, `h3`이 구성원이고 `min_alive = 2`예요. `vm-a`는 `h1`에, `vm-b`는 `h2`에 `runs_on`으로 얹혀 있고 둘 다 `failover_to`가 `ha-1`이에요.

| `dead` | `ha-1` | `vm-a` | `vm-b` |
|---|---|---|---|
| `h1` | `a=2`: A, `slack=0` | `interrupted` (호스트는 죽었지만 넘겨받을 묶음이 산다) | `exposed_direct` |
| `h1`, `h2` | `a=1 < 2`: D | `down`, `lost_channels: [service]` | `down` |
| `h1`, `min_alive`를 3으로 높임 | `a=2 < 3`: D | `down` | `h2`가 살아 있어 `down`이 아니다(넘겨받을 묶음만 죽었다) |
| `h1`, `min_alive`가 `assumed` (사실만 쓴 답) | 1, 2면 A, 3이면 D라 U | `unknown` (묶음의 여유를 모른다) | `unknown` |

첫 줄에서 `ha-1`은 여유가 0이라 `exposure`가 `h2`, `h3`을 후보로 내요. 호스트가 죽고 VM이 다시 뜨는 사이의 끊김과 남은 호스트의 용량은 따지지 않아요. `model_limits`에 문구가 붙어요.

## 4. MLAG 짝의 스위치 하나가 죽음

`sw-a`, `sw-b`는 `mlag-1`(`min_alive = 1`)의 구성원이에요. `vsw-1`(논리 스위치)은 `mlag-1`에 `realized_by`로, `srv-1`은 `mlag-1`에 `supplied_by`(network)로 기대요. `core-1`은 닻이에요. 이어짐 간선: `core-1`-`sw-a`, `core-1`-`sw-b`, `sw-a`-`sw-b`, `srv-1`-`sw-a`, `srv-1`-`sw-b`.

**(가) `dead = [sw-a]`.** 기댐: `mlag-1`의 `a=1 >= 1`이라 A, `slack=0`. 이어짐: 살아 있는 간선은 `core-1`-`sw-b`, `sw-b`-`srv-1`이고 `sw-b`, `srv-1`에 닿아요. 결과: `vsw-1`과 `srv-1`은 `exposed_direct`, 잃은 채널은 없어요. `exposure`는 `sw-b`를 후보로 내요.

**(나) `dead_links = [core-1-sw-a, core-1-sw-b]`**(코어 쪽 업링크 둘).

| 계산 | 결과 |
|---|---|
| 기댐만 (죽은 객체가 없으므로) | `sw-a`, `sw-b` 삶, `mlag-1` A. 모든 것이 산다 |
| 세기만 (서로를 산 이웃으로 셈) | `sw-a`는 `sw-b`가, `sw-b`는 `sw-a`가 산 이웃이라 둘 다 삶. 같은 오답 |
| 이어짐 (`core-1`에서 출발) | `core-1`에 닿는 살아 있는 간선이 없다. `sw-a`, `sw-b`, `srv-1` 모두 `no` |
| 번갈아 돌림 | 닿지 못한 `sw-a`, `sw-b`가 `network`를 잃어 `down`. `mlag-1`의 `a=0`: D. `vsw-1`은 `down`(`lost_channels: [service]`), `srv-1`은 `down`(`lost_channels: [network]`) |

(나)에서 기댐만 쓰면 낙관적으로 틀린 답이 나와요. 이것이 두 계산을 가르는 까닭이고, kg-queries.md 7절의 시험이 이 모양의 입력으로 지켜요.

## 5. N+1 냉방 묶음에서 하나가 죽음

`cool-grp-1`은 방식 `count`(`n_plus_1`)인 `RedundancyGroup`이고 구성원은 `crah-1`~`crah-3`, `min_alive = 2`예요. 홀의 서버 `srv-21`~`srv-23`은 `cool-grp-1`에 `supplied_by`(cooling)로 기대고, 이 링크의 `delay_class`는 `delayed`예요. 각 서버 위에서 VM이 하나씩 돌아요(`runs_on`).

| `dead` | `a` | `cool-grp-1` | 서버 셋 | 그 위의 VM |
|---|---|---|---|---|
| `crah-2` | 2 | A, `slack=0` (`exposure` 후보 `crah-1`, `crah-3`) | `exposed_direct` | `exposed_inherited` |
| `crah-2`, `crah-3` | 1 | D | `down`, `lost_channels: [cooling]`, `delay_class: delayed`(늦게 나타남) | `down` |

`exposure`는 후보마다 `would_die_count = 3`을 내요. 이제 `min_alive = 2`가 **`assumed`**(냉방 용량을 소스가 주지 않아 관리자가 적은 값)라고 해 봐요. 사실만 쓴 답에서 `min_alive`는 모름이라 1, 2, 3을 다 따져요. `dead = [crah-2]`에서는 `a=2`, `n=3`이에요. `min_alive`가 1이나 2면 A, 3이면 `a+u < 3`이라 D이므로 답이 갈려서 **모름**이에요. 가정을 넣은 답은 A(`slack=0`)라 서버는 `exposed_direct`, VM은 `exposed_inherited`예요. 그래서 서버 셋은 사실만 쓴 답에서 `unknown`, 가정을 넣은 답에서 `exposed_direct`예요.
