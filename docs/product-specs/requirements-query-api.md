---
title: 프락시움 요구사항 — 질의와 API
kind: spec
status: active
follows:
  - docs/design-docs/adr/0005-separate-product-own-model-verification-outside.md
---
# 요구사항 — 질의와 API

질의와 REST API(140번대), 사전 정의 질의, 오류 응답, 응답 헤더, 비동기 작업과 드라이 런의 요구사항이다. 목표와 REQ 번호의 규칙은 [요구사항 입구](requirements.md)에 있다. 번호는 모든 요구사항 문서에서 하나뿐이다.

| ID | 요구사항 |
|---|---|
| REQ-140 | **예정(ADR-0010)** 질의는 사전 정의 질의(closed queries) 13종이다. 모든 응답에 KG 버전, 신선도, 근거 등급, 잘렸는지가 붙는다. 첫 3개(`find`, `get`, `neighbors`)의 몫은 REQ-250부터 REQ-254까지이고, 그것을 경로로 제공하는 몫은 REQ-255와 REQ-256이다. |
| REQ-141 | **예정(ADR-0010)** 영향 범위는 링크 의미와 이중화 그룹의 3값으로 계산한다. 사실만 쓴 응답과 가정을 포함한 응답을 따로 주고, 정할 수 없는 것은 알 수 없음으로 준다. `impact`(연결성 제외)의 몫은 REQ-300부터 REQ-306까지이다(ADR-0043). 연결성(`connected_to`)과 `exposure`, `paths`는 남아 있다. |
| REQ-142 | **예정(ADR-0010)** 스위치의 ECMP·BGP·LACP까지 고려한 영향 범위는 1초 안에, 설비 문제가 VM·앱·서비스에 미치는 범위는 수 초 안에 응답한다. 클로즈드 베타에서 측정해서 확인한다. |
| REQ-143 | REST API는 버전이 있는 계약(OpenAPI 3.1)이고 계약이 먼저다. 계약은 `core.contract`에 조각(fragment)으로 두고 기계가 병합해 내장한다. 하네스가 계약의 규칙(`API001`~)을 린트하고, 병합한 것이 조각과 같은지, 호환성을 깨는 변경에 선언이 있는지, 아직 없는 경로의 등록부가 맞는지, 서버가 계약대로 응답하는지 차단한다(ADR-0024). |
| REQ-144 | **예정(ADR-0010)** 새 KG 버전에서 바뀐 것을 다른 시스템에 알리는 변경 알림을 발행한다. |
| REQ-145 | **예정(ADR-0010)** 질의 엔진은 여러 인스턴스로 기동하고, 스토리지가 중단돼도 로드해 둔 버전으로 응답한다. |
| REQ-146 | **예정(ADR-0010)** 질의는 조회 기준(`as_designed`, `as_observed`)을 선택할 수 있고 `max_staleness`보다 오래된 값은 쓰지 않는다. 첫 3개 질의가 지키는 몫은 REQ-252다. |
| REQ-147 | **예정(ADR-0016)** 모든 기능을 API와 CLI로 사용할 수 있다. 알파 1의 몫은 REQ-245이고, KG를 읽는 몫은 REQ-255부터 REQ-257까지, 개발 모드에서 KG를 기동해 읽는 몫은 REQ-260이다. |
| REQ-148 | API의 형식이 일관된다: 오류(문제 상세 본문과 코드 목록), 응답 엔벨로프와 `result_complete`, 페이지네이션(세대가 포함된 커서), 조건부 읽기(ETag), 추적 ID, 상태 확인 경로. 서버 골격이 계약대로 응답하는지 테스트가 차단한다. 비동기 작업, 멱등성 보장, 낙관적 동시성 제어, 한도는 REQ-243이다. |
| REQ-149 | **예정(ADR-0016)** REST와 MCP는 같은 유스케이스 위의 두 어댑터이고, 권한 판단과 응답 엔벨로프가 같다. KG 조회의 MCP 인터페이스는 영구히 읽기만 한다. 게이트웨이(REQ-450)는 다른 주소의 다른 구성요소다. |
| REQ-226 | **예정(ADR-0010)** 기대하는 값과 기준 시각을 주면 속성마다 `confirmed`, `not_yet`, `contradicted`, `unverifiable`로 응답하고, 확인의 근거는 소스에서 관측한 값(`observed`)뿐이다. |
| REQ-229 | 화면이 규칙을 복제하지 않도록 `/v1`이 카탈로그(`/v1/catalog/...`)와 현재 가능한 액션(`meta.allowed_actions`), 필터(`filter[...]`)로 거른 목록을 제공한다. 나머지(저장하지 않는 드라이 런, 시각화에 적합한 형태, SSE)는 REQ-244다. |
| REQ-230 | **예정(ADR-0006)** 질의 엔진은 Go로 직접 구현하고 기존 그래프 엔진을 쓰지 않는다. 합성 세계 생성기와 기준 구현을 비교하는 테스트로 정확성을 검증하고, 규모는 벤치마크로 성능 퇴행을 잡는다. |
| REQ-241 | 이전 이름은 별칭으로 받는다(API가 이전 이름을 별칭으로 받는다). 유지 기간은 REQ-262다. REQ-233에서 분리한 요구이고 API 계약이 담당한다. |
| REQ-242 | **예정(ADR-0016)** 화면은 다른 리포지토리에서 이 계약으로만 백엔드와 통신한다. 계약에서 클라이언트 타입을 생성하고, 화면이 쓰는 경로가 계약에 없으면 차단한다. REQ-143에서 분리한 요구다. |
| REQ-243 | **예정(ADR-0016)** 오래 걸리는 작업은 비동기 작업(`202`와 `Location`)으로, 쓰기는 `Idempotency-Key`로 중복을 방지하고, 수정은 `If-Match`로 동시 수정을 방지하고, 호출에는 한도가 있다. REQ-148에서 분리한 요구다. |
| REQ-244 | **예정(ADR-0016)** 저장하지 않는 드라이 런(`dry_run`), 시각화에 적합한 형태, SSE를 제공한다. REQ-229에서 분리한 요구다. |
| REQ-250 | `find`는 객체 타입(하위 타입을 포함하거나 제외할 수 있다), 특성, 속성(`equals`, `in`, `prefix`, `range`), 식별자, 텍스트로 찾고, 객체 ID 순서로 `limit`와 `cursor`로 페이지네이션한다. 페이지 도중에 KG가 바뀌어도 커서가 처음 읽은 변경 번호에서 이어지고, 다른 질의의 커서와 세대가 바뀐 커서는 거절한다. 마스킹된 속성, `volatile` 속성, `scope_id`를 찾는 조건으로 쓰면 목록에 있는 고정된 코드로 거절한다. REQ-140에서 분리한 요구다(ADR-0026). |
| REQ-251 | `get`은 객체 ID나 식별자로 객체 하나를 읽고 `include`(`attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`)만 제공한다. 속성마다 값, 단위, 등급, 근거 종류, 관측한 시각, `stale`이 붙고, 한도보다 오래된 값은 쓰지 않으며, 링크는 수만 준다. REQ-140에서 분리한 요구다(ADR-0026). |
| REQ-252 | 첫 3개 질의의 모든 응답에 엔벨로프가 붙는다: `as_of`(KG의 ID, 세대, 변경 번호, 확정 버전), `volatile_as_of`, 신선도와 근거 등급(사용한 값에서 계산), `result_complete`, `hidden`. 최신, 변경 번호, 확정 버전으로 읽을 수 있고 과거 읽기는 `volatile` 값을 제공하지 않는다. `min_seq`와 `wait_timeout`을 받고, `as_designed`와 `as_observed`를 선택하며 `max_staleness`를 지킨다. REQ-140, REQ-146, REQ-222, REQ-232에서 분리한 요구다(ADR-0026). |
| REQ-253 | 첫 3개 질의의 응답은 읽기 권한 포트(`ReadFilter`)를 거친다. 읽을 수 없는 객체는 나오지 않고 `get`은 없는 객체와 같은 오류로 응답하며, 수를 내는 모든 필드는 마스킹된 것을 뺀 수이고, 한도를 넘는 속성은 "마스킹됨"이다. 마스킹된 것의 수는 전체 범위의 호출자에게는 수로, 그 밖에는 있음/없음으로 준다. REQ-140에서 분리한 요구다(ADR-0026). |
| REQ-254 | `neighbors`는 방향, 링크 타입, 링크 의미, 상대 타입으로 필터링하고 `max_steps`(기본 1, 상한 3)까지 탐색해, 그룹(`groups`)을 먼저 주고 `items`를 `limit`와 `cursor`로 페이지네이션한다. 마스킹된 객체를 통과해서도 탐색하고 그 수를 `hidden`에 센다. REQ-140에서 분리한 요구다(ADR-0026). |
| REQ-255 | KG를 읽는 GET 경로가 계약대로 제공된다: KG 목록(`/v1/kgs`)과 단건(`/v1/kgs/{kg}`, 별칭 `main`), 객체 목록(`/objects`)과 단건(`/objects/{object_id}`), 객체의 링크(`/links`). 페이지네이션, 조건부 읽기(ETag, 응답 내용의 해시), `meta.allowed_actions`, 문제 상세 본문과 고정된 코드를 따른다. 읽기 권한 필터링은 호출자의 역할을 읽기 권한 포트에 전달하는 것이고, 스토리지가 없으면 `503`이다. 경로는 `adapter.memstore`를 읽지 않고 스토리지 포트만 쓴다. REQ-140과 REQ-147에서 분리한 요구다(ADR-0027). |
| REQ-256 | 사전 정의 질의의 첫 3개가 `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`로 제공된다. 본문의 모르는 필드와 잘못된 필드는 필드별 오류로, 본문이 JSON이 아니면 `415`와 `422`로, 질의의 거절은 고정된 코드로 응답한다. 응답 엔벨로프는 `meta`로 옮겨지고(`as_of`가 `kg`와 `volatile_as_of`를 포함하고 `hidden`이 `has_hidden`을 단다), POST의 응답에는 ETag를 주지 않는다. REQ-140에서 분리한 요구다(ADR-0027). |
| REQ-258 | 개발용 적재기(`adapter.devload`)가 생성기의 정답(JSON Lines)을 적용기로 한도가 있는 변경 배치에 나눠 KG에 넣고 수를 알린다. 적재한 뒤 모든 정답 객체와 링크가 질의로 읽히고, 수가 매니페스트와 맞고(적재할 수 없는 것은 따로 센다), `neighbors`가 정답과 같고, 다시 적재하면 같은 해시와 버전 ID가 나온다. 개발자의 편의와 테스트를 위한 경로이고 수집 파이프라인이 아니다. `praxium dev load-truth`와 `api serve -dev-truth`로 호출한다(ADR-0027). |
| REQ-259 | 핵심 유닛(`core.*`)은 어댑터를 읽지 않는다. 질의와 그 위의 유닛의 테스트는 스토리지 포트의 최신 상태만 보유한 구현(`graph.ScratchStore`)에서 실행되고, 이전 변경 번호와 이전 버전으로 읽는 테스트는 어댑터를 읽어도 되는 테스트 전용 유닛(`adapter.readcheck`)에 있다. 최신만 보유한 스토리지는 과거 시점을 읽으라는 요청을 `unsupported`로 거절한다(ADR-0027). |
| REQ-260 | `api serve -mode development`는 메모리 스토리지를 만들고 `-dev-truth`로 정답 파일을 적재한 KG(`dev`, 별칭 `main`)를 제공한다. 파일이 없으면 읽을 KG가 없다. 적재할 수 없는 파일이면 서버가 기동하지 않는다. `-store postgres -store-dsn-file <파일>`이면 PostgreSQL 스토리지를 쓴다(REQ-270). `-mode production`은 내장 인증과 운영 스토리지가 갖춰져야 시작한다(REQ-311). 갖춰지지 않으면 무엇이 없는지 이름을 대며 거부한다. REQ-245에서 분리한 요구다(ADR-0027). |
| REQ-262 | **예정(ADR-0028)** 이전 이름의 별칭은 한 메이저 버전 동안만 받고, 그다음 메이저 버전에서는 거절한다. 테스트가 이 기간을 세려면 계약이 버전 번호를 알고 별칭마다 끝나는 버전을 적어야 한다. REQ-241에서 분리한 요구다. |

| ID | 요구사항 | 근거·테스트 |
|---|---|---|
| REQ-290 | 서버의 오류 응답은 대상과 이유를 `params`에 싣는다: `not_found.resource`에 `resource`와 `id`, 질문의 값 오류(`invalid_input.field`)에 사람이 읽는 `message`와, 아는 경우 `field`. CLI는 이것을 오류 한 줄에 보여 준다(`field "object_type" (--type): no object type "X"`). 운영 모드 오류는 내부 부채 번호 없이 `praxium demo`, `praxium local`, `-mode development` 중 무엇을 하면 되는지 말한다. `demo`는 `-addr`의 형식 오류를 가장 먼저 말한다 | ADR-0041, `backend/internal/httpapi/errordetail_test.go`, `backend/internal/cli/cause_test.go`, `backend/cmd/praxium/help_test.go` |
| REQ-295 | `/v1`의 응답은 `X-Content-Type-Options: nosniff`와 `Referrer-Policy: no-referrer`를 가진다(JSON 응답이라 CSP는 두지 않는다). 릴리스의 `SHA256SUMS`는 파일 이름순으로 정렬하고, 릴리스 확인 스크립트는 `local`과 `ingest init → ingest run -data-dir → local` 흐름을 확인한다 | ADR-0041, `backend/internal/httpapi/errordetail_test.go` |
