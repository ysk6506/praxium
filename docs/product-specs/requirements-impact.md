---
title: 프락시움 요구사항 — 영향 범위
kind: spec
status: active
follows:
  - docs/design-docs/adr/0005-separate-product-own-model-verification-outside.md
---
# 요구사항 — 영향 범위

영향 범위 질의의 계산(300번대)과 화면(350번대, 374~381)의 요구사항이다. 목표와 REQ 번호의 규칙은 [요구사항 입구](requirements.md)에 있다. 번호는 모든 요구사항 문서에서 하나뿐이다.

영향 범위를 보이는 화면에 연결하기 전의 순수 계산(`apps/web/src/data`)과 그래프·목록 부품(`apps/web/src/ui`)이다. 화면과 실제 API 호출은 아래 374번대가 이었다. 지금의 모양은 임시이고 남는 것은 동작, 계산, 부품의 구조, 접근성 규칙이다(ADR-0051).

부품(350번대)을 실제 화면에 잇는다: 객체 상세의 입구, 주소, 실제 API를 부르는 데이터 훅, 화면. 모양은 임시이고 동작과 문구가 요구사항이다.

| ID | 요구사항 |
|---|---|
| REQ-300 | `impact`는 실패한 객체(`dead`), 끊긴 링크(`dead_links`), 사이트 장애(`site_failure`, 담긴 객체까지)에서 시작해 링크 의미(`runs_on`, `requires`, `supplied_by`, `realized_by`, `part_of`, `member_of`, `managed_by`, `contained_in`)대로 장애를 전파하고, 상태를 채널마다 3값으로 최소 고정점까지 라운드별로 구한다. 응답은 장애가 없을 때와 비교한 차이이고, 장애가 없는 상태에서는 아무것도 바뀌지 않는다. `connected_to`의 연결성은 계산하지 않는다. REQ-141에서 분리한 요구다(ADR-0043). |
| REQ-301 | 이중화 그룹은 구성원 수와 `min_alive`로 계산한다. 정상 구성원이 `min_alive` 이상이면 그룹은 정상이고, 여유(slack)가 줄면 `exposed_direct`(그룹이 여유를 잃음), `exposed_inherited`(그것에 기대는 객체), `degraded`(일부만 잃음)로 알린다. `min_alive`를 모르면 가능한 모든 값에서 같을 때만 값을 낸다. 용량 모드의 그룹은 구성원이 모두 정상일 때만 정상이고, 아니면 알 수 없음과 경고 `capacity_group_unknown`을 준다. REQ-141에서 분리한 요구다(ADR-0043). |
| REQ-302 | `mode`는 `facts_only`, `with_assumptions`, `both`이다. 사실만 쓴 응답은 `assumed` 링크를 알 수 없음으로 읽고, 가정 포함 응답은 사실처럼 쓴다. 약한 링크(가정, 알 수 없음, `max_staleness`를 넘은 관측)는 알 수 없음만 넘기고 장애는 넘기지 않는다. 사실만 쓴 응답에서 확정된 결과는 가정 포함 응답에서도 같고, 가정에 기댄 항목에는 `assumption_based`와 가장 약한 등급을 붙인다(ADR-0043). |
| REQ-303 | 항목마다 결과(`down`, `interrupted`, `unknown`, `exposed_direct`, `exposed_inherited`, `degraded`, `unmanageable`)와 잃은 채널, 직전 원인(`via`), 원인까지 거슬러 올라간 경로(링크마다 의미, 등급, 근거)를 준다. 필요한 채널에 `supplied_by` 링크가 없는 객체는 알 수 없음이 아니라 항목의 `unfilled_channels`로 나열하고 결과 요약에 `unfilled_channel_objects`로 센다. 수는 읽을 수 없는 객체를 뺀 수이고 경로의 숨김 단계는 `hidden`으로만 나온다(ADR-0043). |
| REQ-304 | 요청마다 `max_hops`, `max_items`(기본 5000), `time_budget`(기본 30초)으로 계산을 제한한다. 제한에 걸리면 `result_complete`에 이유(`max_hops`, `time_budget`)를 싣고 `computation`에 완료 여부와 멈춘 이유를 준다. 개수 요약은 항목 상한과 상관없이 전체를 센다. 시드가 1,000개를 넘거나, 없거나, 읽을 수 없는 객체이거나, 방향과 어긋난 필드가 있거나, `dead_scope_id`를 쓰면 고정된 코드로 거절한다(ADR-0043). |
| REQ-305 | `direction: upstream`과 `target_object_id`로 대상이 기대는 객체를 구하고, 후보마다 그 하나만 장애가 났을 때의 결과를 두 읽기 모두에서 계산해 `single_point_of_failure`를 준다. 후보는 2,000개까지이다(ADR-0043). |
| REQ-306 | `POST /v1/kgs/{kg}/queries:impact`가 계약대로 제공되고(계약 조각, 합쳐진 `openapi.json`, 서버, 웹 생성 코드가 일치), `praxium query impact`가 요약, 영향 종류별 묶음, 경로 순서의 사람용 출력과 `--json`과 사용 예와 읽는 법이 있는 `--help`를 제공한다. 같은 질문의 답은 메모리, 파일, PostgreSQL 스토어에서 같다(ADR-0043). |

| ID | 요구사항 | 근거·테스트 |
|---|---|---|
| REQ-350 | 영향 범위 응답의 항목은 시작 객체를 뿌리로 하는 원인 트리가 된다. 항목의 바로 앞 원인이 부모이고, 경로에만 있는 객체는 경유 노드, 읽을 수 없는 객체는 이름 없는 숨김 노드이며, 위쪽 방향은 대상 객체가 뿌리인 같은 트리다. 노드는 층을 열로, 트리를 깊이 먼저 도는 순서를 줄로 놓이고, 같은 입력이면 항목이 어떤 순서로 도착해도 같은 좌표다. 자식은 부모보다 위에 놓이지 않고 한 칸에 두 노드가 놓이지 않는다 | ADR-0051, `apps/web/src/data/impact-tree.test.ts` |
| REQ-351 | 노드가 300개를 넘으면 그래프는 같은 부모 묶음 아래의 같은 타입 노드를 하나의 묶음으로 보인다. 모든 노드는 묶음 하나에 들어가 묶음의 개수 합이 노드 수와 같고, 묶음은 개수와 등급 구성과 구성원(심각한 순)을 가진다 | ADR-0051, `apps/web/src/data/impact-group.test.ts` |
| REQ-352 | 퍼짐 재생의 상태(단계, 재생, 정지)와 전이는 순수 함수이고 시간은 밖에서 받는다. 한 박자는 250~700ms이고 전체는 4초를 넘지 않는다(단계가 많으면 한 박자에 여러 단계를 보인다). 재생·멈춤·다시 보기·슬라이더 이동이 있고, 등급별·단계별 개수와 단계의 설명 문장을 센다 | ADR-0051, `apps/web/src/data/impact-spread.test.ts`, `apps/web/src/data/impact-rows.test.ts`, `apps/web/src/data/impact-picture.test.tsx` |
| REQ-353 | 영향 범위 화면의 질문(시작 객체, 위치 단위 장애, 끊긴 링크, 대상)과 보기 상태(읽기, 단계 한도, 받는 항목 수, 목록·그래프, 확대, 선택, 단계, 정렬, 쪽, 등급)는 `#/kg/{kg}/impact?...`의 쿼리에 담긴다. 기본값은 쓰지 않고, 모르는 값과 형식이 틀린 ID는 기본값으로 읽으며, 쓴 주소는 같은 상태로 읽힌다. 화면은 요약 요청과 항목 요청 둘을 이 상태에서 만든다 | ADR-0051, `apps/web/src/data/impact-view.test.ts` |
| REQ-354 | 그래프와 범례와 목록은 등급을 모양, 글자, 색으로 함께 나타내고 색은 테마의 토큰만 쓴다. 간선은 부모에서 자식으로 가는 꺾은선이고 단계의 그룹 안에서 모양별로 `<path>` 하나에 이어 붙이며(장식이라 `aria-hidden`), 근거 등급을 선 모양(실선, 점선)으로, 이중화 그룹을 거침을 선 위의 표지로 나타낸다. 경로에서 가장 약한 근거와 보이지 않는 객체를 거침은 목록 행과 노드의 이름에 나온다. 단계마다 층 하나(`<g>`)의 투명도로 나타나고 아직 닿지 않은 층은 키보드와 화면 낭독기에서 빠진다. 화면에는 등급 이름을 "이중화 상실"로 쓴다 | ADR-0051, `apps/web/src/ui/impact-graph.test.tsx`, `apps/web/src/data/impact-rows.test.ts` |
| REQ-355 | 그래프는 이름 있는 영역이고 안의 노드는 이름(이름, 타입, 층, 등급, 단계)을 가진 버튼이다. 탭 정지는 하나이고 방향키(오른쪽 자식, 왼쪽 부모, 위아래 같은 열, Home과 End)로 옮기며 Enter와 Space로 고르고 Esc로 푼다. 고른 노드는 재생 중이 아닐 때 알림 영역이 읽고, 목록으로 건너뛰는 단추가 있다 | ADR-0051, `apps/web/src/ui/impact-graph.test.tsx` |
| REQ-356 | 움직임을 줄인 설정에서는 처음 열 때 재생하지 않고 끝 단계로 열리며 단계 사이의 전환이 없다. 재생·멈춤·다시 보기·슬라이더는 그대로 동작하고 재생하면 한 박자씩 즉시 바뀐다 | ADR-0051, `apps/web/src/data/impact-spread.test.ts`, `apps/web/src/data/impact-picture.test.tsx`, `apps/web/src/ui/impact-graph.test.tsx` |
| REQ-357 | 영향 범위의 목록은 받은 항목 전부를 등급별 구역(심각한 순)과 정렬 가능한 열로 보이고, 구역마다 이름 있는 표와 개수("전체 M개 중 K개 표시")를 가진다. 구역은 처음 50개를 보이고 구역별 "더 보기"가 50개씩 이어서 보이며 쪽 번호와 펼침 상태의 주소 저장은 없다. 항목이 없을 때는 없다는 사실을 알린다 | ADR-0051, `apps/web/src/data/impact-rows.test.ts`, `apps/web/src/ui/impact-graph.test.tsx` |
| REQ-374 | 객체 상세에서 영향 범위 화면(`#/kg/{kg}/impact?...`)을 세 방식으로 열 수 있다: "이 객체가 멈추면"(아래 방향), "이 객체가 의존하는 것"(위 방향, 단일 장애점 포함), 그리고 다른 객체를 담는 타입의 객체에는 "안에 든 것까지 멈추면"(`site_failure`). 어떤 타입이 담는 타입인지는 모델이 정한다(링크 타입의 의미가 `contained_in`인 바인딩의 안쪽 끝과 그 하위 타입). 화면은 타입 이름을 적어 두지 않는다. 주소가 가리키는 화면과 보기 상태는 새로 고침과 뒤로 가기에서 같은 화면이다 | ADR-0054, `apps/web/src/impact-screen.test.tsx`, `apps/web/src/data/impact-model.test.ts`, `apps/web/src/data/impact-route.test.ts` |
| REQ-375 | 화면은 `POST /v1/kgs/{kg}/queries:impact`를 먼저 두 읽기의 개수 요약으로, 이어서 고른 읽기의 항목을 페이지마다(받는 항목 수 기본 2,000) 받는다. 화면을 떠나거나 질문이 바뀌거나 사람이 중지하면 진행 중인 요청을 중단하고, 중지하면 받은 항목까지로 그린다. 읽기만 바꾸면 요약은 다시 묻지 않는다 | ADR-0054, `apps/web/src/data/useImpact.test.tsx`, `apps/web/src/impact-screen.test.tsx` |
| REQ-376 | 화면 머리는 무엇이 멈췄다고 가정했는지(객체 이름과 타입, 객체 상세로 가는 링크), 방향, 읽는 방식(사실만, 가정 포함)을 보이고 읽는 방식을 바꿀 수 있다. 바꾸면 새 질문으로 쳐서 뒤로 가기가 이전 읽기로 돌아간다 | ADR-0054, `apps/web/src/impact-screen.test.tsx` |
| REQ-377 | 등급별 개수는 받은 항목 수가 아니라 서버가 센 전체 수이고, 모양과 글자가 있는 표지로 보인다. 받은 항목이 전체보다 적으면 그렇다고 말한다. 개수를 누르면 그 등급만 그래프와 목록에 보이고 다시 누르면 풀린다. 등급 이름은 "이중화 상실(직접)"과 "이중화 상실(간접)"이다 | ADR-0054, `apps/web/src/data/impact-notices.test.ts`, `apps/web/src/impact-screen.test.tsx` |
| REQ-378 | 결과는 그래프(첫 탭)와 목록의 두 탭으로 보인다. 그래프에서 노드를 고르면 이름, 타입, 등급, 잃은 것, 시작 객체부터의 경로, 근거 등급과 객체 상세로 가는 링크를 보이고, 묶음 노드를 고르면 구성원을 100개까지 보인다. 읽을 수 없는 객체는 이름 없이 숨김 객체로 보이고 링크를 열지 않는다. 진입, 그래프 탐색, 노드 선택, 객체 상세로의 이동이 키보드만으로 된다 | ADR-0054, `apps/web/src/impact-screen.test.tsx` |
| REQ-379 | 결과가 잘리면 이유를 구별해 알린다: 항목 수 한도(한도를 올려 다시 묻는 단추), 시간 한도, 단계 한도(한도 없이 다시 묻는 단추), 사람이 불러오기를 중지함(다시 받는 단추). 필요한 채널에 `supplied_by` 링크가 없는 객체 수(`unfilled_channel_objects`)와 그 뜻, 계산에 가정을 썼다는 사실, 서버의 계산 메모와 "이 계산이 보지 않는 것"을 함께 보인다 | ADR-0054, `apps/web/src/data/impact-notices.test.ts`, `apps/web/src/impact-screen.test.tsx` |
| REQ-380 | 영향받는 객체가 없으면 "없음이 안전하다는 뜻은 아니다"를 말하고 계산이 보지 않는 것을 보인다. 오류는 다른 화면과 같은 방식으로 보인다. 위 방향의 결과는 단일 장애점을 서버가 센 수와 함께 따로 표시한다 | ADR-0054, `apps/web/src/impact-screen.test.tsx` |
| REQ-381 | 접속 설정 화면은 "개발용 토큰"이 아니라 "접근 토큰"이라고 말하고, 데모, 로컬 모드, 개발 서버, 운영 서버 각각에서 토큰을 어디서 얻는지 한 줄씩 안내한다(운영 서버는 관리자가 `praxium admin token create`로 만든 `pxm_…` 토큰) | ADR-0054, `apps/web/src/impact-screen.test.tsx` |
