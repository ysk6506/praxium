---
title: 표준 모델의 바인딩과 이중화 상실의 설계
kind: guide
status: active
follows:
  - docs/design-docs/adr/0059-model-links-and-redundancy-loss.md
---
# 표준 모델의 바인딩과 이중화 상실의 설계

[ADR-0059](adr/0059-model-links-and-redundancy-loss.md)의 자세한 내용이에요. **1절(바인딩 우선순위)과 2절(값이 정해지는 순서, 바인딩의 기본값)은 구현했고([ADR-0065](adr/0065-binding-priority-and-order-of-param-values.md)), 나머지는 아직 구현하지 않은 설계(예정)예요.** 구현하지 않은 부분은 이 문서의 "지금 코드" 열이나 "코드를 읽은 판단"으로 적은 대로 움직여요. 숫자와 판정은 조사에서 나온 것이고 공개 문서로 하나하나 확인하지 않았어요(7절).

## 1. 바인딩 우선순위의 정의

링크 타입 하나에 맞는 바인딩이 여럿이면 양쪽 끝이 더 좁은 바인딩이 이겨요(예전에는 바인딩 id 문자열이 가장 작은 하나가 이겼어요). `cardinality`, `failover_to`, 인자가 모두 그 바인딩에서 나와요. 이 판단은 `model` 패키지의 비교 함수 하나(`better`)에 있고, `BindingFor`, `BindingsFor`(같은 링크 타입에서는 첫 항목이 `BindingFor`의 답이에요), 수집의 카디널리티 검사, 영향 범위의 인자 읽기가 모두 그 결과를 써요. 조직이 더한 정의에도 같은 규칙이 걸려요.

- 끝의 점수: any는 0, 트레이트는 1, 타입은 2 + 깊이예요. 깊이는 조상 타입의 수이고 뿌리 타입은 0이에요. 타입의 하위 타입도 맞는 것으로 봐요.
- 바인딩의 점수: 시작 끝과 도착 끝의 점수의 합이에요. 큰 쪽이 이겨요.
- 점수가 같으면 바인딩 id 순이에요. 표준 모델에는 같은 점수가 없어요(모든 타입 쌍을 계산해 확인했어요).
- 알려진 조합이 먼저예요. 알려진 조합이 하나도 맞지 않을 때만 일반 규칙(3절)을 써요.

### 예 셋

| 링크 | 맞는 바인딩과 점수 | 이기는 것 |
|---|---|---|
| `Chiller` POWERED_BY `PowerRedundancyGroup` (`Chiller` 깊이 0, `PowerRedundancyGroup` 깊이 1) | 트레이트(전원을 받는 것)→전원 원천 트레이트 2, `Chiller`→전원 원천 트레이트 3, 트레이트(전원을 받는 것)→`PowerRedundancyGroup` 4 | 마지막(4). 예전에는 id 순이라 `chiller`→전원 원천 트레이트가 이겼어요. |
| `StorageArray` UPLINKED_VIA `SanSwitch` (`StorageArray` 0, `SanSwitch` 1) | `storage_array`→`network_device` 4, `storage_array`→`san_switch` 5 | `san_switch`(5). 예전에는 `network_device`가 이겼어요. |
| `VirtualMachine` MANAGED_BY `VirtualizationManager` (둘 다 깊이 0) | 트레이트(관리 대상)→`virtualization_manager` 3, `virtual_machine`→`virtualization_manager` 4 | `virtual_machine`(4). 카디널리티가 N:M에서 N:1이 되는 예라서 바인딩을 N:M으로 넓혔어요(아래). |

### 효과의 크기 (확인한 수)

표준 모델의 모든 타입 쌍을 계산해 확인했고, 테스트(`binding_priority_test.go`, 목록은 `testdata/binding_winners.txt`)가 고정해요.

- 맞는 바인딩이 둘 이상인 (링크 타입, 시작 타입, 끝 타입) 조합이 226개예요. 이 가운데 20개에서 이기는 바인딩이 바뀌어요. 설계 때의 추정과 같아요.
- POWERED_BY 11개: `Transformer`→`UtilityFeed`, `TransferSwitch`→`PowerRedundancyGroup`, `Ups`→`Switchgear`, `Ups`→`TransferSwitch`, `Ups`→`PowerRedundancyGroup`, `Chiller`·`CoolingTower`·`Crah`·`Crac`·`InRowCooler`·`Cdu`→`PowerRedundancyGroup`. OWNED_BY 4개: `Tenant`→`Team`, `OnCallSchedule`→`Team`, `Contract`→`Organization`, `Circuit`→`Organization`. REALIZED_BY 2개: `TechnicalService`→`Workload`, `ApplicationService`→`Workload`. 그리고 MANAGED_BY 1개(`VirtualMachine`→`VirtualizationManager`), UPLINKED_VIA 1개(`StorageArray`→`SanSwitch`), IN_FAILURE_DOMAIN 1개(`ClusterNode`→`FailureDomain`)예요.
- 20개 모두 인자가 같아서 영향 범위의 답은 바뀌지 않아요. 모든 타입 쌍의 카디널리티와 인자(`strength`, `whole_needs_part`, `resource`, `failover_to`)를 코드를 고치기 전에 떠 둔 표(`testdata/binding_answers.txt`)와 같은지 테스트가 확인해요.
- 카디널리티가 N:M에서 N:1이 되는 것은 `VirtualMachine`→`VirtualizationManager`, `Circuit`→`Organization`, `ClusterNode`→`FailureDomain` 셋이에요. 좁아지는 셋 가운데 둘(`VirtualMachine`→`VirtualizationManager`, `ClusterNode`→`FailureDomain`)은 새로 이기는 좁은 바인딩을 N:M으로 넓혀서 기존 데이터를 새로 거부하지 않아요. `Circuit`→`Organization`은 식별 문맥 때문에 N:1로 둬요. `Circuit`의 식별 키가 OWNED_BY를 문맥으로 쓰고, 모델 검증이 `Circuit`에서 시작하는 N:1 바인딩을 요구하기 때문이에요. 그래서 소유 조직이 둘 이상인 회선 자료는 둘째 링크가 문제 목록으로 빠져요(수집의 카디널리티 검사, 테스트로 고정). 나머지 둘을 좁히는 시점은 미결 사항이에요.

## 2. 링크별 인자와 값이 정해지는 순서 (0단계, 링크에 쓴 값은 예정)

**구현한 부분**: 값이 정해지는 순서를 `model.ResolveParams` 한 함수가 정하고(`Registry.ParamsFor`가 바인딩을 뽑아 그 함수를 불러요), 영향 범위가 그것을 불러요. 2번(링크에 쓴 값)은 함수의 인자(`written`)로 자리만 있고 지금은 아무도 값을 넘기지 않아요. 바인딩의 기본값은 `param_defaults`예요. 모델 검증은 두 가지를 봐요. 기본값은 그 인자의 허용 값 안에 있어야 하고, 허용 값이 여럿인데 의미의 기본값이 그 안에 없으면 `param_defaults`가 반드시 있어야 해요.

링크 하나가 의존의 세기(`strength`)와 전체가 부분을 필요로 하는가(`whole_needs_part`)의 값을 직접 가질 수 있어요. 개수(`cardinality`)는 바인딩의 속성이라 링크가 정하지 않아요. `failover_to`를 링크마다 둘지는 이 설계에 없어요. 링크의 어느 필드에 쓰는지, 수집 매핑 문법, 저장, 값의 근거 등급은 0단계 전에 정해요(10절). 값은 이 순서로 정해져요.

1. 링크 타입이 고정한 값(`resource` 등)
2. 링크에 쓴 값. 바인딩이 허용하는 값 안에서만 받아요.
3. 바인딩의 기본값(새 항목)
4. 바인딩이 허용하는 값이 하나뿐이면 그 값
5. 링크 의미의 기본값. 지금 코드와 같아서 `strength`는 hard, `whole_needs_part`는 none이에요.

제약이 없는 바인딩(`DEPENDS_ON`, `SECURED_BY`, `PART_OF`의 대부분)은 5번을 써요. 값이 여럿 허용된 바인딩을 soft로 읽던 규칙은 없앴어요. 해당하던 것은 `PART_OF` `VirtualDisk`→`VirtualMachine`(`{hard, soft}` 허용) 하나이고, 이 바인딩에 `param_defaults`로 soft를 적어서 영향 범위의 답이 바뀌지 않게 했어요. 0단계의 완료 기준은 모든 타입 쌍의 유효한 카디널리티와 인자가 전과 같은 것이에요. 수집의 검사는 바뀔 수 있어요(우선순위가 이기는 바인딩을 바꾸기 때문이에요).

## 3. 바인딩: 알려진 조합과 일반 규칙

- 지금의 바인딩은 알려진 조합이에요. 조사에서 거부된 흔한 조합은 알려진 조합으로 올려요(6절).
- 일반 규칙은 알려진 조합이 하나도 맞지 않을 때만 써요. 이 규칙으로만 통과한 링크는 거부하지 않고 알림을 남겨요. 알림은 문제로 세지 않고 따로 세어 요약에 보여요. `-fail-on-problem`은 알림으로 실패하지 않아요. 알림도 실패로 다루는 선택 항목은 뒤의 개선이에요.

| 규칙 | 내용 | 비고 |
|---|---|---|
| 1 | 누구나 그룹의 구성원이 될 수 있어요. 시작 끝은 any예요. | "장애가 날 수 있는" 것으로 좁히는 트레이트는 미결 사항이에요. 모든 타입에 `tr.common`이 붙어 있어서 그 트레이트로는 좁혀지지 않아요. |
| 2 | 그룹이 그룹의 구성원이 될 수 있어요. | 순환은 금지해요. 검사를 어디에 두는지는 미결 사항이에요. |
| 3 | 구성원에게 의존할 수 있는 곳에서는 그룹에도 의존할 수 있어요. | `runs_on`, `requires`, `supplied_by`, `realized_by`, `managed_by`에 적용해요. |

모든 그룹 하위 타입(`Lag`, `RaidGroup`, `EcmpGroup`, `HaPair`, `QuorumGroup`, `VirtualChassis`, `ReplicationGroup`, `PowerRedundancyGroup`, `ComputeCluster` 등)은 `RedundancyGroup` 아래에 있어요. `LogicalGroup`은 그 아래에 없어요.

경로를 이어 주는 것과 필요를 선언하는 것은 달라요. `tr.network_attached` 같은 트레이트를 붙이면 hard 필요를 선언하고 빈 채널을 만들어요. 끝이 any인 바인딩으로 할지, 필요를 선언하지 않는 새 트레이트로 할지는 미결 사항이에요. 관리망의 규칙도 더해요. BMC가 없는 관리 경로(스위치의 관리 포트가 관리 스위치에 붙은 경우)도 적을 수 있어야 해요.

### 그룹이 장비에 속하는 `PART_OF`

hard만 허용하는 아래 여섯 바인딩은 `whole_needs_part`의 기본값을 `none`으로 하고 허용 값을 hard와 none으로 해요. soft를 허용할지는 미결 사항이에요.

`RedundancyGroup`→`PhysicalServer`, `RedundancyGroup`→`Chassis`, `RedundancyGroup`→`NetworkDevice`, `RedundancyGroup`→`StorageArray`, `HaPair`→`StorageArray`, `RaidGroup`→`StorageArray`.

엣지 라우터의 `HaPair`(첫 라우터의 `PART_OF`, `supplied_by` 없음)도 같은 모양이에요. 답은 바뀌지 않아요(쌍이 내려가면 그 라우터도 이미 내려가 있어요, 추정).

지금은 hard라서 `supplied_by` 링크가 없는 HA 쌍이나 RAID 그룹이 내려가면 장비 전체가 내려가요. 바꾸면 장비는 정상으로 남고, 그 그룹에 기대는 링크가 있는 볼륨과 데이터스토어만 내려가요. 그 링크가 없는 자료에서는 컨트롤러 둘이 모두 내려가도 어레이가 정상으로 남아요. 장비가 그룹을 필요로 한다는 사실은 의존의 링크가 말해야 해요. 그래서 기본값을 바꾸는 릴리스는 이 의존 링크를 표준 모델의 알려진 조합, 합성 세계, 표현 안내에 함께 넣어요(정확한 링크 타입은 한 릴리스 전에 정해요).

기본값을 바꾸는 릴리스에는 두 조건이 있어요. (가) 합성 세계에서 답이 달라지는 것을 모두 나열해 이유를 대요. (나) 사용자의 기존 자료에서 "기본값이 바뀌어 답이 중단에서 영향 없음으로 달라질 수 있는 링크"를 찾아 주는 점검 수단을 함께 내고 릴리스 노트에 방법을 적어요.

## 4. 점대점 연결

뜻은 링크 타입이 아니라 **바인딩 단위**로 켜요. 대상은 네트워크의 포트와 포트뿐이에요. 장비와 장비의 이웃 관계와 전원에는 켜지 않아요. 인자의 이름은 구현 때 정해요.

- 두 끝은 서로를 필요로 해요. 순환은 최소 고정점이 풀어요.
- 상대가 둘 이상이면(브레이크아웃, 대장과 이웃 데이터가 어긋남) 알 수 없음이에요. 서로 다른 상대의 수로 세요. 같은 상대를 가리키는 `CABLED_TO`와 `ADJACENT_TO`는 하나로 세요.
- 중간에 케이블 객체가 있으면 포트는 케이블을 필요로 하고 케이블은 양쪽 끝을 필요로 해요. 두 끝이 모두 네트워크 포트일 때만 적용하고 전원 케이블에는 적용하지 않아요.
- 패치 포트는 통과로 봐요. 먼 쪽을 찾는 방법은 미결 사항이에요.
- 포트에서 장비 전체로 번지지 않아야 해요. 장비의 일부인 `Lag`의 기본값이 none이 되는 것(3절)이 그 장치예요.
- `as_designed`는 관측한 이웃을 쓰지 않으므로 관측 링크(`ADJACENT_TO`)에는 이 뜻이 적용되지 않아요. 선언된 케이블 대장의 `CABLED_TO`에는 적용돼요. 한계예요.
- 켠 바인딩의 링크만 세요. 끝이 둘이 아닌 케이블(브레이크아웃)에는 적용하지 않아요. 자세한 것은 한 릴리스 전에 정해요(10절).

### 바인딩 표 (`links.json`을 읽어 만든 것)

| 바인딩 | 켬/끔 | 이유 |
|---|---|---|
| `CABLED_TO` `Interface`↔`Interface` | 켬 | 네트워크 포트와 포트예요. |
| `CABLED_TO` `Interface`→`PatchPort` | 보류 | 패치 포트의 먼 쪽을 찾는 방법이 미정이에요. |
| `CABLED_TO` `PatchPort`↔`PatchPort` | 보류 | 같은 이유예요. |
| `ADJACENT_TO` `Interface`↔`Interface` | 켬 | 포트와 포트의 이웃 관계예요. |
| `ADJACENT_TO` `NetworkDevice`↔`NetworkDevice` | 끔 | 장비와 장비의 이웃 관계라서 포트의 대응이 없어요. |
| `TERMINATES_AT` `Cable`→`Interface` | 켬 | 케이블의 모든 끝이 네트워크 포트일 때만이에요. |
| `TERMINATES_AT` `Cable`→`PatchPort` | 보류 | 패치 포트의 통과 규칙이 정해지기 전이에요. |
| `TERMINATES_AT` `Cable`→`PduOutlet` | 끔 | 전원이에요. |
| `TERMINATES_AT` `Cable`→`PowerSupply` | 끔 | 전원이에요. |
| `TERMINATES_AT` `Cable`→`PowerFeed` | 끔 | 전원이에요. |
| `TERMINATES_AT` `Circuit`→`Interface` | 끔 | 회선은 포트가 아니에요. 회선이 끊기는 것은 그룹의 구성원으로 표현해요. |

집계: 켬 3, 끔 5, 보류 3(모두 11).

`connected_to` 의미의 다른 링크 타입(`ALLOWS_VLAN`, `MAPS_TO`, `EXPORTED_TO`, `REPLICATES_TO`, `CONNECTED_TO`)의 바인딩 15개도 모두 꺼요.

## 5. 이중화 상실의 계산

### 5.1 정의

- 그룹의 여유는 정상 구성원 수 − `min_alive`예요. 원인이 되는 그룹은 "이번 질문의 장애 때문에 정상 구성원이 줄어 여유가 0이 된 그룹"이에요(`kg-queries.md` 5.7절과 지금 코드의 뜻 그대로). 질문과 무관하게 원래 여유가 0이던 그룹은 원인으로 세지 않아요. `min_alive`를 모르는 그룹과 용량 기준 그룹도 대상이 아니에요. 이런 그룹이 "내려간다"고 가정했을 때 지금 정상인 객체 가운데 내려가는 것이 이중화 상실이에요.
- 그룹마다 따로 계산하지 않아요. 그룹이 내려가는 것을 원인으로 두고 원인의 표시를 달아서 지금의 장애 전파 경로(`part_of`, 그룹의 수, `managed_by` 포함)를 따라 한 번에 전파해요. 비용은 그래프 크기에 비례해요.
- 원인이 하나이면 확실한 상실이에요. 그룹에 직접 의존하면 "직접", 아니면 "간접"이에요. 서로 다른 원인이 함께 있어야 내려가면 "이중화 상실 가능"이에요. 결과 코드의 이름은 구현 때 정해요.
- 새 결과는 지금 답과의 합집합이에요. 페일오버를 선언한 사용자를 표시하는 지금의 규칙을 그대로 둬요. 그룹이 내려가도 페일오버 호스트는 내려가지 않기 때문이에요. 원인 표시는 지금 정상인 객체에만 붙여요(이미 `interrupted`인 객체는 그대로예요).
- 부품은 항목으로 나열하지 않고 장비 항목의 수로 합쳐요. 부품의 기준은 타입 이름을 읽지 않고 정해야 하고 미결 사항이에요.

아래 세 그래프는 지금 코드와 새 정의의 답을 나란히 놓은 손 계산이에요. **"지금 코드" 열은 코드를 읽은 판단이고, 새 테스트를 돌려서 얻은 것이 아니에요.** 새 정의의 열은 설계이고 구현되지 않았어요.

### 5.2 예 1: 한 원인으로 내려가는 경우

그래프. PDU-A와 PDU-B가 있어요. PSU-1은 PDU-A에서, PSU-2는 PDU-B에서 `POWERED_BY`로 전원을 받아요. PG는 `PowerRedundancyGroup`(`min_alive` 1)이고 PSU-1과 PSU-2가 구성원이에요. ARR(`StorageArray`)은 PG에서 `POWERED_BY`를 받고, OST(`ObjectStore`)는 ARR에서 `RUNS_ON`이고, BKT(`ObjectBucket`)는 OST의 `PART_OF`예요. 이 바인딩은 모두 있어요. 시드는 PDU-A가 내려간 것이에요.

| 대상 | 지금 코드(코드를 읽은 판단) | 새 정의(설계) |
|---|---|---|
| PSU-1 | 내려감 | 내려감 |
| PG | 정상(a=1, `min_alive`와 같아서 여유 0) | 여유 0이라 원인이 돼요 |
| ARR | exposed_direct | 직접, 원인 PG, 확실 |
| OST | exposed_inherited | 간접, 원인 PG, 확실 |
| BKT | 표시 없음. `part_of`는 노출됨을 전파하지 않아요. | 간접, 원인 PG, 확실. 별도 항목인지 OST 항목에 합치는지는 부품의 기준(미결)에 달려요. |

### 5.3 예 2: 두 원인이 함께 필요한 경우

그래프. 서버 A와 B가 그룹 H(`min_alive` 1)의 구성원이고 서비스 svc가 H에 `RUNS_ON`이에요(일반 규칙 3으로 통과해요). A는 G1에서, B는 G2에서 `POWERED_BY`를 받아요. G1과 G2는 각각 PSU 둘(`min_alive` 1)이고, G1의 PSU 하나는 PDU1a에서 다른 하나는 PDU1b에서, G2의 PSU 하나는 PDU2a에서 다른 하나는 PDU2b에서 전원을 받아요. 시드는 PDU1a와 PDU2a가 내려간 것이에요.

| 대상 | 지금 코드(코드를 읽은 판단) | 새 정의(설계) |
|---|---|---|
| A | exposed_direct | 직접, 원인 G1, 확실 |
| B | exposed_direct | 직접, 원인 G2, 확실 |
| H | 변화 없음(a=2) | 이중화 상실 가능(원인 G1과 G2가 함께 필요) |
| svc | 표시 없음 | 이중화 상실 가능(원인 G1과 G2) |

### 5.4 예 3: 페일오버 그룹의 지금 답이 지켜지는 경우

그래프. 호스트 h1, h2, h3와 클러스터 C(`min_alive` 2)가 있어요. vm1은 h1에, vm2는 h2에 `RUNS_ON`이고 둘 다 `failover_to`가 C예요. app은 vm2에 `RUNS_ON`이에요. 시드는 h1이 내려간 것이에요.

| 대상 | 지금 코드(코드를 읽은 판단) | 새 정의(설계) |
|---|---|---|
| vm1 | interrupted | interrupted(이미 `interrupted`라 원인 표시를 붙이지 않아요) |
| vm2 | exposed_direct(페일오버로 C를 쓰는 사용자) | exposed_direct. 합집합이라 유지해요. |
| app | exposed_inherited | exposed_inherited |

C가 내려간다고 가정해도 페일오버 호스트는 내려가지 않아서 새 규칙만으로는 vm2가 표시되지 않아요. 그래서 지금의 규칙을 합집합으로 남겨요.

### 5.5 찾지 못하는 경우

여유는 남았지만 남은 구성원이 한 설비에 몰린 경우는 `impact`도 `exposure`도 찾지 못해요. 다른 질의가 다룬다고는 말하지 않아요. 이중화 상실을 계산한 응답에는 이 한계를 `model_limits`에 넣어 답에 적어요. 공통 설비에 몰린 구성원을 찾는 일은 뒤로 미뤘어요([14절의 한계](kg-queries.md)에도 적었어요).

### 5.6 일부 손실

분산 스토리지에서 허용한 수를 넘게 잃으면 "내려감"이 아니라 "알 수 없음(일부 손실)"이에요. 그룹 계산 방식을 하나 더해요(이름은 구현 때). 결과 종류는 `unknown`이고 이유를 붙여요. 새 결과 종류로 만들지는 구현 설계에서 정해요.

## 6. 조사에서 거부된 연결과 처리

두 조사 보고서에서 모은 것이에요. 보고서에 없는 연결은 넣지 않았어요. 다만 `PowerSupply` PART_OF `StorageArray`는 ADR-0057에서 왔고, 오버레이 터널의 AND 묶음은 거부된 연결이 아니라 표현의 문제라서 아래에 적은 대로 따로 뒀어요. "지금"은 현재 `links.json` 기준이에요.

| 연결 | 처리 | 이유 |
|---|---|---|
| `PhysicalServer`, `HypervisorHost`, `ClusterNode`가 `HaPair`의 구성원 | 일반 규칙 1(알림) | 구성원은 누구나 될 수 있어요. 흔한 것은 알려진 조합으로도 올려요. |
| `ArrayController`가 `QuorumGroup`의 구성원 | 일반 규칙 1 | 위와 같아요. |
| `HaPair`가 `HaPair`나 `QuorumGroup`의 구성원 | 일반 규칙 2 | 그룹이 그룹의 구성원이에요. |
| `NetworkDevice`, `Router`가 `EcmpGroup`의 구성원 | 일반 규칙 1 | |
| `LineCard`, `LoadBalancerAppliance`가 `HaPair`의 구성원 | 일반 규칙 1 | |
| `Circuit`이 `EcmpGroup`의 구성원 | 일반 규칙 1 | |
| `PhysicalServer`가 `RaidGroup`의 구성원 | 일반 규칙 1 | |
| `Datastore` BACKED_BY `ReplicationGroup`, `RaidGroup` | 일반 규칙 3 | 구성원에게 의존할 수 있는 곳이에요. |
| `VirtualDisk`, `StoragePool`, `Endpoint` RUNS_ON `ReplicationGroup` | 일반 규칙 3 | |
| `Endpoint`, `StoragePool` RUNS_ON `HaPair` | 일반 규칙 3 | |
| `LbVirtualServer` RUNS_ON `HaPair` | 일반 규칙 3 | |
| `PhysicalServer`, `HypervisorHost` UPLINKED_VIA `HaPair`, `VirtualChassis` | 일반 규칙 3 | |
| `StorageArray` UPLINKED_VIA `HaPair` | 일반 규칙 3 | |
| `NetworkDevice`, `HypervisorHost` MANAGED_BY `QuorumGroup` | 일반 규칙 3 | |
| `DnsZone` RUNS_ON `HaPair` | 일반 규칙 3 | |
| `DnsZone` RUNS_ON `SoftwareInstance`(여러 개) | 여전히 거부 | 카디널리티가 N:1이에요. 그룹의 형태(`HaPair`)가 대신해요. |
| `StoragePool` BACKED_BY `StorageVolume` | 알려진 조합으로 올림 | 설계의 판단이에요. |
| `VirtualDisk` BACKED_BY `StorageVolume`, `StoragePool` | 알려진 조합으로 올림 | |
| `Chassis` PART_OF `StorageArray` | 알려진 조합으로 올림 | |
| `PowerSupply` PART_OF `StorageArray` | 알려진 조합으로 올림 | ADR-0057이 한계로 남긴 것이에요. |
| `Endpoint`의 `ObjectStore`, `FileShare` 링크 | 알려진 조합으로 올림 | |
| 볼륨, 디스크, 공유, 오브젝트 스토어 MANAGED_BY 관리 서비스 | 알려진 조합으로 올림 | |
| `Bmc` UPLINKED_VIA `NetworkDevice` | 알려진 조합으로 올림 | 관리망의 규칙이에요. |
| `VirtualMachine`→`VirtualNetwork`, `VirtualNetwork`→`VirtualSwitch`, `VirtualSwitch`→`HypervisorHost`의 UPLINKED_VIA | 알려진 조합으로 올림 | |
| `ClusterNode` UPLINKED_VIA `NetworkDevice`, `HypervisorHost` | 알려진 조합으로 올림 | 연결 경로예요. |
| `Router` RUNS_ON `VirtualMachine` | 알려진 조합으로 올림 | 확인하지 못한 변형이에요(변형 실험은 하지 않았어요). |
| `PhysicalServer`, `OperatingSystem`, `VirtualMachine`의 DEPENDS_ON | 알려진 조합으로 올림 | 강도는 링크마다 써요. |
| `Interface` RUNS_ON `Cable` | 새 바인딩 없음 | 4절의 `TERMINATES_AT` `Cable`→`Interface`의 점대점이 같은 답을 줘요. |
| `Interface` RUNS_ON `PatchPort` | 보류 | 패치 포트의 통과 규칙이 정해지기 전이에요(4절의 보류). |
| `Interface` RUNS_ON `Circuit` | 새 바인딩 없음 | 회선이 끊기는 것은 그룹의 구성원으로 표현해요(4절의 끔). |
| `Interface` UPLINKED_VIA `SanSwitch` | 여전히 거부 | SAN 경로는 결정이 없어요. 존, 마스킹, 멀티패스를 의존 대상으로 삼는 것도 같아요. |
| `PhysicalServer` LOCATED_IN `Site` | 여전히 거부 | 결정이 없어요. |
| `OperatingSystem`, 서버의 `DnsZone`, `OverlaySegment` 의존 | 여전히 거부 | 결정이 없어요. |
| 한 `SoftwareInstance`가 여러 그룹에 속함 | 여전히 거부 | N:1 알려진 조합(`ha_pair`, `quorum_group`, `redundancy_group`)이 우선순위로 이겨서 같은 바인딩의 두 번째 그룹은 거부돼요. 미결 사항이에요. |

오버레이 터널을 AND로 묶는 것은 거부된 연결이 아니라 표현의 문제이고 결정이 없어요.

지금도 통과하는 것: `Lag` MEMBER_OF `EcmpGroup`, `StorageArray` UPLINKED_VIA `Lag`, `VirtualSwitch` PART_OF `HypervisorHost`, `NetworkDevice` MANAGED_BY `Bmc`.

## 7. 조사의 한계

- 구성 25가지(저장 13, 네트워크 12), 장애 질문 약 163개(저장 75, 네트워크 88), 틀린 답 58건(각 29건)이에요. 숫자는 대략이에요.
- 타입별 계산이 필요한 경우는 두 쪽 모두 0건이에요.
- 판정은 조사자의 일반 지식으로 내렸어요. 검토가 공개 문서로 맞춘 것은 스케일아웃 NAS의 보호 수준, 분산 스토리지의 최소 복제 수, 하이퍼컨버지드의 과반 규칙, Redfish의 이중화 스키마, NetBox의 케이블이에요. 약 160개 질문은 하나하나 맞추지 않았어요.
- 그래서 "실제 답"을 공개 문서로 확인한 뒤(8절의 순서 2)에야 현실의 답을 단언하는 시나리오 테스트를 잠가요.

## 8. 답이 바뀌는 변경과 순서

| 순서 | 변경 | 답 |
|---|---|---|
| 0 | 0단계: 링크별 인자, 값의 순서, 우선순위 | 영향 범위의 답은 같아요. 수집의 검사는 바뀔 수 있어요. |
| 1 | 알려진 조합과 일반 규칙, 알림 | 영향 범위는 같아요. 거부되던 링크가 들어와요. 그룹의 그룹을 열면 한 릴리스 전까지 이중화 상실이 안쪽 그룹에서 멈춰요(한계). |
| 2 | 실제 답을 공개 문서로 확인 | 현실의 답을 단언하는 테스트를 잠그기 전이에요. |
| 3 | 한 릴리스: 점대점, 이중화 상실의 재정의, 일부 손실, ECMP `min_alive` 바로잡기, 그룹이 장비에 속하는 `PART_OF`의 기본값(3절의 조건과 의존 링크 포함) | 답이 바뀌어요. 시나리오 테스트는 변경과 같은 PR에 써요. MLAG의 피어 링크처럼 그룹이 아닌 것의 같은 문제는 시나리오 테스트로 찾아 같은 방식으로 다뤄요. "서버가 죽어도 ToR은 산다"와 "MLAG 한 대가 죽어도 짝은 산다"를 none을 적지 않은 그래프로 단언해요. |
| 4 | 구성 가이드 | 확인된 구성만이에요. |
| 5 | 합성 세계의 다양화, 관리 도구 소스 | 인스턴스를 확인한 뒤예요. |

테스트의 원칙: 지금 틀리는 답의 테스트를 미리 넣지 않아요. 답을 바꾸는 변경과 그 시나리오 테스트는 같은 풀 리퀘스트에 넣어요. 그 앞 단계에는 지금도 맞는 답의 테스트와 바인딩 허용의 테스트만 들여요.

### 합성 세계에서 달라질 것으로 예상되는 답 (모두 추정)

- 서버 NIC↔ToR, ToR↔집계, 집계↔스파인, 컨트롤러↔ToR의 `CABLED_TO` `Interface`↔`Interface`에 점대점이 켜지면 `Lag`와 ECMP의 수가 반대쪽에 반응해요. 지금은 스파인이 모두 내려가도 집계는 영향이 없지만 바뀐 뒤에는 네트워크를 잃어요. 반대쪽 포트가 목록에 "내려감"으로 나와요.
- ECMP의 `min_alive`가 1이 되어 용량이 줄면 degraded나 exposed로 보여요.
- 그룹이 장비에 속하는 `PART_OF`의 기본값 none은 `supplied_by` 링크가 없는 곳에서만 답을 바꿔요. `HaPair`→어레이와 `RaidGroup`→어레이예요. 지금은 어레이 전체가 내려가지만 바뀐 뒤에는 어레이가 정상으로 남고 그 쌍이나 RAID 그룹이 만든 볼륨과 데이터스토어가 내려가요. 전원 장치, 팬, ToR의 `Lag`, ECMP는 이미 `supplied_by` 링크가 있어요. 합성 세계에서 확인할 것: 엣지 `HaPair`가 첫 라우터의 `PART_OF`라 그 라우터 하나의 고장에 쌍이 내려가는 것으로 읽혀요(확인되지 않은 추정).
- 부품을 합치지 않으면 서버마다 항목이 15개 넘게 늘어난다는 것이 검토의 추정이에요.
- 골든 세계의 해시가 ECMP 값 때문에 바뀌어요.
- 고쳐야 할 것으로 예상되는 테스트(구현하는 결정이 고쳐요): `backend/internal/worldgen/golden_test.go`, `backend/internal/model/standard_test.go`의 해시와 개수 테스트, `backend/internal/readcheck/impact_scenarios_test.go`와 `impact_scenarios2_test.go`, `backend/internal/query/impact_*_test.go`.

## 9. 가상화 관리 도구를 소스로 쓸 때

- 확인된 것(공개 문서): 어댑터가 선언하는 관계는 부모/자식뿐이고 의미 이름이 없어요. 수집 주기는 기본 5분이에요. 값은 마지막으로 수집한 값과 시각이에요. 인벤토리의 변경 알림은 문서에서 찾지 못했어요. 물리 NIC, 가상 NIC, 가상 디스크는 이름 붙은 속성 묶음으로 와요.
- 확인되지 않은 것(추정): 호스트별 분산 스위치의 업링크, 부모 사슬 전체, 식별자의 내용.
- 관계 매핑 표는 소스 정의에 두고 (어댑터 종류, 부모 리소스 종류, 자식 리소스 종류)를 링크 타입과 방향에 이어요. 표에 없는 쌍은 문제 목록에 쌍마다 한 줄 남겨요.
- 표와 추가할 타입의 목록은 실제 인스턴스에서 확인되기 전에는 초안이에요. 공개 리포지토리에는 확인된 것만 둬요.
- 호스트의 유지보수 속성은 이미 있어요(`tr.schedulable`의 `maintenance_state`).

## 10. 미결 사항

ADR-0059의 미결 사항과 같아요. 어느 단계 전에 정해야 하는지를 붙였어요.

**0단계 전(링크에 쓴 값)**: 링크가 인자 값을 갖는 필드, 수집 매핑 문법, 저장, 값의 근거 등급. 0단계의 완료 기준 테스트와 모델 해시 테스트를 고치는 결정 기록은 ADR-0065가 닫았어요.

**1단계 전**: 일반 규칙 3의 정확한 모양(출발 타입의 범위, 적용할 링크 타입, 카디널리티, 인자의 기본값). 일반 규칙의 표시 방법. 그룹 순환 검사의 위치(정하지 않으면 한계로 적어요). 올릴 바인딩의 정확한 목록. 알림을 모으는 단위. 관리망의 규칙과 역할을 모르는 네트워크 연결의 기본 처리. 필요를 선언하지 않는 경로 트레이트와 끝이 any인 바인딩 중 선택. "장애가 날 수 있는" 타입의 트레이트. `SoftwareInstance`의 N:1. 세 바인딩의 카디널리티를 좁히는 시점.

**한 릴리스 전**: 점대점의 세는 법과 상대가 둘 이상일 때 값이 번지는 것. 원인 표시의 규칙(원인이 따로따로 닿을 때, 원인 하나를 고르는 규칙, 가정과 약한 링크의 처리, `kg-queries.md` 7절 "노출됨" 불변식의 새 문장). 그룹이 장비에 속하는 `PART_OF`가 none일 때 장비가 그룹을 필요로 한다고 말할 의존 링크의 정확한 링크 타입과 합성 세계의 hard. 그 `PART_OF`의 soft 허용. 끈 것 때문에 답하지 못하는 장면의 한계. "이중화 상실 가능"과 "관리 이중화 상실"의 결과 종류와 표시, 죽은 반대쪽 포트의 표시. 패치 포트와 보류 세 건. 부품의 기준. 임계가 둘인 분산 스토리지의 `min_alive`.

**그 밖**: 속성 묶음의 매핑, 순환이 있을 때 상류 질의의 경로 모양, SAN 경로와 `LOCATED_IN` `Site`와 `OverlaySegment` 의존.
