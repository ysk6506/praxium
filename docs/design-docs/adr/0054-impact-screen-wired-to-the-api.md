---
title: ADR-0054 영향 범위 화면의 연결 — 주소, 읽기 훅, 화면, 객체 상세의 입구
kind: decision
status: active
enforced_by:
  - apps/web/src/impact-screen.test.tsx
  - apps/web/src/data/useImpact.test.tsx
  - apps/web/src/data/impact-notices.test.ts
  - apps/web/src/data/impact-model.test.ts
  - apps/web/src/data/impact-route.test.ts
  - TLOCK001
  - TRACE001
amends_tests:
  - apps/web/src/data/impact-rows.test.ts::[REQ-354] a grade is a shape, a word and a color, never a color alone > calls the loss of redundancy by that name, and tells a dashed line from a solid one
test_change_reason: 화면의 등급 이름 이중화 상실(상속)을 이중화 상실(간접)으로 바꾸기로 했다. 직접과 짝이 맞는 쉬운 말이라는 제품 책임자의 판단이고, 이름 하나만 바뀐다. 단언의 수와 강도는 그대로다.
updates_docs:
  - docs/product-specs/requirements.md
  - docs/design-docs/glossary.md
  - docs/design-docs/temporary-console.md
  - ARCHITECTURE.md
touches_protected:
  - docs/product-specs/requirements.md
  - harness/config/test-lock.json
  - docs/design-docs/adr/0051-impact-view-parts-svg-without-new-dependencies.md
amends:
  - adr: "0051"
    what: 간접 노출 등급의 화면 이름
decision_hash: 8d1e1c3d20d54bf3
---
# ADR-0054 영향 범위 화면의 연결 — 주소, 읽기 훅, 화면, 객체 상세의 입구

## 맥락

ADR-0051이 영향 범위 화면의 계산(`data`)과 부품(`ui`)을 만들었고, 엔진의 API(ADR-0043)가 병합됐다. 이 결정은 둘을 잇는다: 객체 상세에서 영향 범위를 열고, 실제 `POST /v1/kgs/{kg}/queries:impact`를 부르고, 결과를 질문 머리, 등급별 개수, 그래프와 목록, 알림으로 보인다. 화면의 모양은 여전히 임시이고(디자인 콘셉트는 정하지 않았다) 남는 것은 동작, `data`의 계산과 읽기, 부품의 구조, 접근성 규칙, 문구다.

## 결정

1. **주소와 입구.** 화면의 주소는 `#/kg/{kg}/impact?...`이고 질문과 보기 상태는 ADR-0051이 만든 `impact-view.ts`의 쿼리 읽기·쓰기를 그대로 쓴다. 라우트는 `{ page: "impact", kg }`이고 쿼리는 화면이 `useHashParams`로 읽는다(탭, 선택, 정렬 같은 쿼리 변경은 같은 화면이다). 객체 상세의 머리에 입구 셋을 둔다: "이 객체가 멈추면"(`dead`), "이 객체가 의존하는 것"(`target`), 그리고 다른 객체를 담는 타입의 객체에만 "안에 든 것까지 멈추면"(`site`). **담는 타입은 모델이 정한다**: 링크 타입 중 의미가 `contained_in`인 것의 바인딩에서 안쪽 끝(`to`)이 가리키는 타입과 트레이트, 그리고 그 하위 타입이다(`data/impact-model.ts`). 화면에 타입 이름이 없다. 표준 모델에서는 지역, 사이트, 건물, 층, 방, 방 구역, 열, 랙과 장애 도메인이 해당한다.
2. **읽기 훅**(`data/useImpact.ts`). 질문을 두 번 보낸다. 먼저 `shape=summary`, `mode=both`로 두 읽기의 개수와 계산 정보를 받고, 이어서 고른 읽기의 `shape=items`를 쪽마다(쪽당 1,000개, 커서) 받는다. 받는 항목 수의 기본은 2,000이다. 서버가 센 개수가 0이면 항목을 요청하지 않는다. 모든 요청은 `AbortController`의 신호를 갖고, 화면을 떠나거나 질문이 바뀌거나 "받기 중지"를 누르면 진행 중인 요청이 중단된다. 중지하면 받은 항목까지로 그리고 그렇다고 알린다. 읽기만 바꾸면 요약은 다시 묻지 않고 항목만 다시 받는다. 요약은 몇 ms 걸리고 항목은 그동안 화면이 그린 뒤 받는다.
3. **화면**(`screens/ImpactScreen.tsx`, `screens/impact/*`). 질문 머리(멈췄다고 가정한 객체의 이름과 타입과 링크, 방향, 읽는 방식의 선택), 등급별 개수(**서버가 센 전체 수**, 모양과 글자가 있는 표지, 누르면 그 등급만), 알림, 탭 둘(그래프가 첫 탭, 목록), 노드를 고르면 나오는 상세(이름, 타입, 등급, 잃은 것, 시작 객체부터의 경로, 근거, 객체 상세로 가는 링크, 묶음이면 구성원 100개까지). 결과가 없으면 "영향받는 객체가 없습니다. 없다는 것이 안전하다는 뜻은 아닙니다"를 말하고, 오류는 기존 `Async`와 `ErrorState`로 보인다. 위 방향은 단일 장애점을 서버가 센 수와 함께 따로 표시한다. 그래프는 항목을 처음 받은 뒤에 만들어 처음 열 때의 재생이 받은 단계로 돈다.
4. **알림의 문구는 `data/impact-notices.ts`의 순수 함수다.** 잘림을 구별해 한 줄씩 알린다: 항목 수 한도(`items_over_cap`, 한도를 다음 값으로 올려 다시 묻는 단추), 시간 한도(`stopped_by=time_budget`), 단계 한도(`stopped_by=max_hops`, 한도 없이 다시 묻는 단추), 사람이 받기를 중지함(다시 받는 단추). 그 밖에 필요한 채널에 공급 링크가 없는 객체 수(`unfilled_channel_objects`)와 그 뜻, 계산에 가정을 썼다는 사실(가정 포함이거나, 사실만이면 가정·알 수 없는 링크 수), 오래된 관측을 뺀 수, 서버의 계산 메모(`warnings`), 접힌 "이 계산이 보지 않는 것"(`model_limits`, 영어 원문)을 보인다.
5. **등급 거르기는 항목을 걸러 트리를 다시 만든다.** 등급을 누르면 그 등급의 항목만으로 원인 트리를 만들어 그래프와 목록이 같은 것을 보인다. 걸러진 등급의 항목이 경로에만 남으면 "경유"로 그려진다. 읽는 방식을 바꾸면 거르기를 푼다(다른 읽기에는 그 등급이 없을 수 있다).
6. **용어.** 등급 이름 "이중화 상실(상속)"을 "이중화 상실(간접)"로 바꾼다(`exposed_inherited`. "직접"과 짝이 맞는 쉬운 말이다). `impact-grades.ts`, 용어집, 이 이름을 단언하던 잠긴 테스트를 함께 고쳤다. ADR-0051의 표는 확정된 본문이라 고치지 않고 이 ADR이 바꾼다(`amends`).
7. **접속 화면의 문구.** "개발용 토큰"이라는 말을 "접근 토큰"으로 바꾸고 접속 설정 화면이 데모, 로컬 모드, 개발 서버, 운영 서버 각각에서 토큰을 어디서 얻는지 한 줄씩 안내한다. 운영 서버는 관리자가 `praxium admin token create`로 만든 `pxm_…` 토큰이고, 개발 서버는 `-dev-token-file`의 값이다. 같은 칸에 어느 쪽을 붙여 넣어도 동작한다. 오류 안내(`errors.ts`)의 같은 문구도 바꿨다.
8. **하지 않는 것.** API 변경(`counts_by_hops`, `shape=graph`, 등급을 번갈아 뽑는 표본), 확대·이동·작은 지도, 3D, 영향 결과에 조직 속성을 함께 보이기, 내보내기. 새 의존성과 새 유닛이 없다.

## 이유

- **요약과 항목을 나누는 것**은 개수를 항목이 다 올 때까지 기다리지 않고 먼저 보이기 위해서다. 실제 서버에서 요약은 수십 ms였고(UPS 하나의 질문은 항목 2,000개를 쪽 둘로 받는다), 개수가 서버가 센 전체라서 받은 항목이 한도로 잘려도 개수는 맞다.
- **모델에서 담는 타입을 읽는 것**은 타입 이름을 화면에 적으면 모델이 바뀔 때(오버레이, 새 타입) 입구가 어긋나기 때문이다. 대신 객체 상세를 열 때마다 타입 목록과 링크 타입 목록, `contained_in` 링크 타입의 바인딩을 읽는다(요청 4개 안팎). 입구 읽기가 실패해도 나머지 둘은 그대로 보인다.
- **걸러서 트리를 다시 만드는 것**은 그래프와 목록이 한 데이터를 보게 해서 둘이 어긋나지 않게 하려는 선택이다. 그래프만 따로 거르면 부모가 없는 노드를 다뤄야 한다.
- **`focus`를 선택한 묶음의 키로 쓴다.** ADR-0051의 주소 설계에서 `focus`는 "확대한 묶음"이었지만 확대는 이번 범위가 아니라서, 개요에서 고른 묶음을 `focus`에 담았다. 객체의 선택은 `sel`이다. 읽을 수 없는 객체의 노드(`hidden:…`)는 주소에 담지 않고 선택하지 않는다.

## 대안과 기각 사유

- 화면에 담는 타입의 이름을 적기: 기각(위).
- 요약 없이 항목만 받기: 개수가 항목이 모두 온 뒤에야 보이고, 한도로 잘리면 개수가 틀린다.
- 쪽 하나에 모두 받기(`limit` 최대): 응답이 커서 첫 그림이 늦어지고 중지할 곳이 없다.
- 화면 테스트를 `screens` 아래에 두기: 구조 계약이 `screens`에서 `test-support`와 `react-dom`을 쓰지 못하게 해서(`web.screens`는 `data`와 `ui`만) 테스트는 이 둘을 쓸 수 있는 `web.app`(`apps/web/src/impact-screen.test.tsx`)에 두었다. 이 파일은 잠금 안이라 새 테스트를 `pnpm lock-tests`로 잠갔다. 구조 계약은 바꾸지 않았다.

## 결과

- 새 요구사항 REQ-374~381, 테스트는 각각 `[REQ-NNN]`으로 가리킨다. 새 파일: `data/impact-model.ts`, `impact-notices.ts`, `impact-counts.ts`, `useImpact.ts`, `ui/Notice.tsx`, `screens/ImpactScreen.tsx`와 `screens/impact/*`.
- 잠긴 테스트 하나를 바꿨다(아래 표).
- 이 화면의 모양(색, 배치, 구성)은 임시이다. 디자인이 정해지면 `ui`와 `screens`를 다시 만들고, `data`의 읽기와 계산과 문구, 접근성 규칙이 남는다.

### 바꾼 잠긴 테스트

| 테스트 | 옛 값 | 새 값 |
|---|---|---|
| `impact-rows.test.ts`의 "calls the loss of redundancy by that name…" | `exposed_inherited`의 이름이 "이중화 상실(상속)" | "이중화 상실(간접)". 같은 테스트의 다른 단언은 그대로 |

### 측정 (`praxium demo`, 합성 세계 7,236개 객체, 같은 기계, 헤드리스 크롬, 화면 열기부터 SVG가 그려질 때까지)

| 질문 | 서버가 센 전체 | 첫 그림까지 | 그래프 |
|---|---|---|---|
| 랙 PDU 하나가 멈추면 | 1,454개 | 0.33~0.40초 | 묶음 51개, 열 9개, 한 열에 묶음 최대 13개 |
| ToR 스위치 하나가 멈추면 | 311개 | 0.19~0.30초 | 묶음 17개, 열 6개, 한 열 최대 4개 |
| 물리 서버 하나가 멈추면 | 19개 | 0.20~0.23초 | 노드 20개(시작 포함), 열 3개, 하드웨어 열에 16개 |
| 물리 서버 하나가 의존하는 것 | 43개(단일 장애점 6개) | 약 0.80초(서버의 후보별 계산) | 노드 44개, 열 4개, 한 열 최대 16개 |
| UPS 하나가 멈추면 | 3,441개(받은 것 2,000개, 잘림 알림) | 0.30~0.32초 | 묶음 58개, 열 9개, 한 열 최대 14개 |

한 열에 개별 노드가 수백 개인 경우는 나오지 않았다(300개를 넘으면 묶음이다). 서버 하나의 결과는 자식 16개가 하드웨어 열에 쌓여 세로가 길다. 콘솔 오류 0건, 외부 요청 0건(390px와 1280px, 라이트와 다크). 번들(gzip): JS 145,400 → 161,012바이트(+15,612, 예산 170,000), CSS 6,368 → 6,425바이트(예산 10,000).
