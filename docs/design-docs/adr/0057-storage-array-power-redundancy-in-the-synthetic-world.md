---
title: ADR-0057 합성 세계의 스토리지 어레이에 전원 이중화 그룹을 둔다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - TRACE006
  - backend/internal/readcheck/impact_scenarios2_test.go
  - backend/internal/worldgen/golden_test.go
updates_docs:
  - docs/design-docs/worldgen.md
  - docs/design-docs/kg-queries.md
  - docs/design-docs/kg-read-measurements.md
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0043-impact-query-propagation-redundancy-and-limits.md
amends:
  - adr: "0043"
    what: 스토리지 어레이가 PDU 둘에 직렬이라 PDU 하나에 내려간다는 설명
amends_tests:
  - go:backend/internal/worldgen::TestGoldenTinyWorld
  - go:backend/internal/worldgen::TestScaleIsClose
  - go:backend/internal/readcheck::TestImpactHAPairOfAnArrayGoesDownWithOnePduBecauseTheArrayIsWiredToBothInSeries
test_change_reason: 합성 세계의 스토리지 어레이가 전원 장치 둘과 전원 이중화 그룹을 갖게 되어 세계가 바뀐다. 골든 세계의 객체와 링크 수와 해시, 규모 검사가 쓰는 가장 작은 규모의 입력, 어레이가 PDU 하나로 내려가는지 단언하던 영향 범위 시나리오의 기대값이 새 세계의 값으로 바뀐다. 단언의 종류와 강도는 그대로이거나 더 세밀해진다
decision_hash: 91c3429db4c0125a
---
# ADR-0057 합성 세계의 스토리지 어레이에 전원 이중화 그룹을 둔다

## 맥락

합성 세계의 스토리지 어레이는 같은 랙의 PDU a와 PDU b에 `POWERED_BY` 링크를 하나씩 직접 걸고 `PowerRedundancyGroup`이 없었다. 모델의 뜻(kg-model 7절)으로는 같은 채널의 피드 링크 여럿은 가장 나쁜 값을 따르고 이중화는 그룹 객체로만 표현한다. 그래서 두 PDU는 어레이에 직렬이었고, 랙 PDU 하나가 멈추면 어레이, 컨트롤러 둘, HA 쌍과 그 위의 데이터스토어와 가상 디스크 수백 개가 "중단"으로 계산됐다. 현실의 이중 전원 어레이는 PDU 하나가 멈춰도 돌아가고 이중화만 잃는다. ADR-0043은 이것을 생성기의 표현 문제로 적고 그대로 두었다. 엔진이 아니라 생성기의 모델링 결함이므로 이제 고친다.

## 결정

어레이의 전원을 서버와 같은 방식으로 만든다. 서버의 전원을 만드는 코드(`dualPower`)를 어레이도 쓴다.

- 어레이마다 `PowerSupply` 둘을 만들고 각각 PDU a와 PDU b에서 `POWERED_BY` 전원을 받는다.
- 둘을 `PowerRedundancyGroup`(`two_n`, `min_alive` 1)에 `MEMBER_OF`로 묶고, 그룹은 어레이의 `PART_OF`다.
- 어레이는 PDU에 직접 걸던 `POWERED_BY` 둘을 없애고 그 그룹에서 `POWERED_BY` 전원을 받는다.
- 표준 모델은 바꾸지 않는다. 표준 모델에는 `PowerSupply`에서 `StorageArray`로 가는 `PART_OF` 바인딩이 없다(서버와 섀시에만 있다). 그래서 어레이의 전원 장치는 어레이의 `PART_OF`로 달지 않고 그룹과 PDU로만 이어진다. 어레이를 포함하는 선택은 표준 모델의 변경이므로 설계 책임자의 결정으로 남긴다.

다른 장비도 조사했다. 같은 결함(한 장비가 PDU 둘에 직접 링크)이 있는 것은 어레이뿐이다.

| 장비 | 전원 연결 | 판단 |
|---|---|---|
| 서버 | 전원 장치 둘 + 2N 그룹 | 이미 맞다 |
| ToR 스위치 | 같은 면의 PDU 하나 | 쌍이 MLAG 도메인으로 이중화된다. 고치지 않는다 |
| 어그리게이션 스위치 | 면의 RPP 하나 | 두 면의 스위치가 ECMP로 이중화된다. 고치지 않는다 |
| 스파인 스위치, 에지 라우터 | 전원 링크 없음 | 직렬 결함이 아니라 전원이 모델에 없는 것이다. 이 변경에서 건드리지 않는다 |
| CRAH, 냉각기 | 면의 RPP 또는 스위치기어 하나 | 냉각 그룹의 N+1로 이중화된다. 고치지 않는다 |
| UPS, 변압기, 스위치기어 | 한 면의 상류 하나 | 면마다 따로 있고 UPS 뱅크 그룹이 있다. 고치지 않는다 |

## 이유

- 같은 모델 규칙을 서버에는 쓰고 어레이에는 안 쓰는 것은 일관되지 않고, 데이터센터를 아는 사람이 데모에서 바로 틀렸다고 본다.
- 서버와 같은 코드를 쓰면 표준 모델의 타입과 링크만으로 끝나고 새 개념이 없다.

## 대안과 기각 사유

- 어레이에 `PowerSupply`를 `PART_OF`로 다는 표준 모델 변경: 모델 변경은 이 작업의 범위가 아니다.
- 어레이 대신 컨트롤러마다 전원을 두는 것: 현실의 이중 전원 어레이는 컨트롤러가 아니라 어레이(섀시)에 전원 장치가 있고, 컨트롤러가 전원 경로를 갖게 하면 영향 범위의 부분-전체 규칙이 바뀐다.
- 엔진이 직렬 링크를 이중화로 읽게 바꾸는 것: 모델 의미(7절)를 깨뜨린다.

## 결과

랙 PDU 하나(스토리지 랙의 PDU a)의 영향 범위를 `praxium demo`와 같은 세계에서 `query impact`로 측정했다(시드 1, 사실만 보는 읽기).

| 규모 | 때 | 중단 | 알 수 없음 | 이중화 상실(직접) | 이중화 상실(간접) | 저하 | 합계 |
|---|---|---|---|---|---|---|---|
| 20,000(데모 기본) | 전 | 681 | 12 | 10 | 137 | 614 | 1,454 |
| 20,000(데모 기본) | 후 | 22 | 0 | 9 | 765 | 0 | 796 |
| 100,000 | 전 | 690 | 12 | 15 | 158 | 623 | 1,498 |
| 100,000 | 후 | 22 | 0 | 9 | 777 | 0 | 808 |

중단은 681에서 22로 줄었다. 남은 22는 이 PDU에서만 전원을 받는 것(ToR 스위치와 그 LAG와 인터페이스, 이 PDU에 걸린 전원 장치)이다. 어레이는 "중단"이 아니라 이중화 상실(직접)이고 그 위의 풀, 볼륨, 데이터스토어, 가상 디스크와 가상 머신은 이중화 상실(간접)이다. PDU 둘이 모두 멈추면 어레이와 컨트롤러 둘과 HA 쌍이 중단이다.

세계의 크기가 어레이마다 객체 3개와 링크 4개 늘어난다(전원 장치 2개와 그룹 1개, 링크는 새로 여섯 개에서 직접 링크 둘을 뺀 것). 시드 7의 작은 세계(규모 500)는 522/932에서 525/936으로, 큰 세계(규모 280,000)는 100,150/178,973에서 100,204/179,045로 바뀌었고, `kg-read-measurements.md`의 객체 수, 링크 수, `volatile` 값 수를 다시 측정했다(식별자 수와 정답 파일 크기는 그대로다). 같은 세계가 0.1%도 크지 않게 커져서 적재와 질의 시간은 다시 측정하지 않았다. 생성기의 결정성과 규모 옵션은 그대로다.

바뀐 잠긴 테스트(`amends_tests`):

| 테스트 | 바뀐 것 | 이유 |
|---|---|---|
| `TestGoldenTinyWorld` | 골든 객체 수 1,391에서 1,394, 링크 수 2,505에서 2,509, 내용 해시와 매니페스트 해시 | 작은 골든 세계의 어레이 하나가 전원 장치 둘과 그룹을 얻었다. 단언은 같다 |
| `TestScaleIsClose` | 가장 작은 규모의 입력 3,000에서 3,100 | 이 규모의 세계는 랙 단위로만 커져서 이미 허용 범위의 끝(3,896개, 상한 3,900)이었고 7개가 늘면 넘는다. 허용 범위(0.7~1.3배)는 그대로 두고 세계가 범위 안에 드는 입력으로 옮겼다 |
| `TestImpactHAPairOfAnArrayGoesDownWithOnePduBecauseTheArrayIsWiredToBothInSeries` | 이름을 `TestImpactAnArrayLosesRedundancyWithOnePduAndGoesDownWithBoth`로 바꾸고 새 사실을 단언 | 옛 사실("PDU 하나로 어레이와 HA 쌍이 중단")이 틀렸다. 새 테스트는 정답 데이터의 구조(그룹과 전원 장치 둘, 각 PDU), PDU 하나에서 어레이는 이중화 상실(직접)이고 풀과 볼륨과 데이터스토어와 가상 디스크와 가상 머신은 이중화 상실(간접)이며 컨트롤러와 HA 쌍은 건드리지 않는 것, PDU 둘에서 어레이와 컨트롤러와 HA 쌍이 중단인 것, 경로의 단계를 단언한다 |
