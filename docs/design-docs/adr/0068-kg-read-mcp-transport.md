---
title: ADR-0068 KG 읽기 MCP의 전송을 구현한다 (ADR-0062의 구현 2단계 앞부분)
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH021
  - backend/internal/mcpapi/table_new_test.go
  - backend/internal/mcpapi/table_old_test.go
  - backend/internal/mcpapi/guard_test.go
  - backend/internal/mcpapi/body_test.go
  - backend/internal/mcpapi/tools_test.go
  - backend/internal/contract/mcp_test.go
touches_protected:
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - docs/design-docs/adr/**
  - docs/product-specs/requirements-query-api.md
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/kg-mcp.md
  - docs/design-docs/defaults.md
  - docs/guides/operate-the-server.md
  - docs/guides/connect-agents.md
  - docs/product-specs/requirements-query-api.md
decision_hash: 026450ee12cf687c
---
# ADR-0068 KG 읽기 MCP의 전송을 구현한다

## 맥락

ADR-0062의 구현 순서 2단계 가운데 가장 불확실한 부분인 전송과 판 처리를 먼저 구현한다. KG를 읽는 툴(`kg_catalog`, `kg_find` 등)과 한도는 다음 변경이다. 이 변경이 끝나면 인증된 에이전트가 세 판(2026-07-28, 2025-11-25, 2025-06-18) 어느 것으로든 `tools/list`와 `tools/call`(`kg_whoami`)을 할 수 있다.

## 결정

- **새 유닛 `adapter.mcp`**(`backend/internal/mcpapi`)는 전송과 판 처리, 출처와 토큰 검사, 툴 등록부만 맡는다. `may_import`는 `core.kernel`, `core.contract`, `core.access`, `core.kgread`이다. `restricted_stdlib`의 `net/http`에 더하고 `net`은 더하지 않는다(주소 문자열은 직접 자른다). `adapter.http`는 `adapter.mcp`를 import하지 않는다. 조립 지점(`cmd.praxium`)이 `httpapi.Config.Mounts`로 핸들러를 서버에 끼운다. 마운트는 추적 ID, 접근 로그, 패닉 복구 안쪽에 있고 REST의 본문 검사 앞에 있다. `/mcp`와 `/.well-known/` 아래 전체가 웹 화면보다 먼저 이 핸들러에 간다. 새 유닛의 커버리지 하한 한 줄을 더한다.
- **본문 파서를 직접 둔다.** 표준 디코더는 키의 대소문자를 구분하지 않고 같은 키가 겹치면 뒤의 것을 쓰므로, 키를 글자 그대로 대조하고 같은 키의 중복, 널 문자가 든 키, 깊이 32를 넘는 중첩, 잘못된 UTF-8을 거절하는 파서를 둔다. 대소문자만 다른 알려진 멤버 이름(`Method` 등)은 그 멤버로 읽지 않고 거절한다. 퍼징 테스트는 표준 라이브러리로 하고 평소 검사에서는 시드 입력(말뭉치)만 돈다.
- **판 처리 표(kg-mcp.md 2.1)의 행마다 테스트 사례를 둔다.** 이름에 행 번호를 적는다. "구현 때 확인"으로 표시한 칸(N11, N12, N13, O5, O8, O9)은 문서의 권고 값으로 구현했고, 실제 클라이언트나 적합성 테스트로 확인하지 못했다. 테스트와 문서에 그 표시를 남긴다. 표에 없어 정한 것: 헤더가 지원하지 않는 판이고 `_meta`는 2026-07-28이면 헤더와 `_meta`가 다른 것이므로 -32020이다(`data.supported`를 싣는다). 알림은 판 확인을 요청과 같게 하고 `_meta`에 능력(`clientCapabilities`)이 있는지만 본다(`Mcp-Method`는 요구하지 않는다). 판 헤더의 확인은 본문을 읽은 뒤에 한다(`initialize`를 가려야 하고 `_meta`와 대조해야 한다). 그 밖의 순서(경로, 메서드, `Origin`과 `Host`, 인증, `Content-Type`, `Accept`, 본문)는 설계 문서의 순서를 따른다. `tools/list`에 커서를 주면 -32602이다. `ttlMs`는 측정 전이라 0이다.
- **받는 토큰과 새 오류 코드.** 역할이 `viewer` 하나뿐인 주체만 받고, 운영 모드에서는 서비스 계정이어야 한다. 그 밖은 HTTP 403 `permission.mcp_not_allowed`이고 `params.reason`이 이유(`roles_must_be_viewer_only`, `must_be_service_account`)이다. 기존 코드 체계(`<그룹>.<세부>`)의 `permission` 그룹에 맞춘 이름이다. 계약의 오류 코드 목록, 병합한 OpenAPI, 웹 화면의 생성 타입을 함께 고쳤다.
- **켜고 끄기.** 플래그는 `-mcp`(켜고 끈다)와 `-mcp-allowed-origins`(쉼표로 나눈 `Origin` 목록)이고 `api serve`, `demo`, `local`이 같은 이름을 쓴다. `-mcp`를 주지 않으면 `api serve`는 모드를 따르고(운영 모드는 꺼짐, 개발 모드는 켜짐) `demo`와 `local`은 켜짐이다. 꺼져 있으면 `/mcp`와 `/.well-known/` 아래는 `not_found.resource` 문제 상세의 JSON 404이다. 민감 등급 상한 플래그(`-mcp-max-sensitivity`)는 그것을 쓰는 툴이 없어서 이 변경에 넣지 않는다(KG를 읽는 툴과 함께 온다).
- **툴 등록부와 계약.** 등록부는 전송을 모르는 모양(주체, 이름, 원본 인자에서 글 블록, 구조화 값, 오류 표시)이다. 툴에는 종류가 있고 `/mcp`는 `kg_read` 종류만 내며, 다른 종류가 등록부에 있어도 목록에도 호출에도 나오지 않는다. 툴의 이름, 설명, 입력과 출력 스키마, 읽기 전용 주석은 `core.contract`의 `mcp/tools.json`에 두고, 코드의 등록부와 어긋나면 테스트가 실패한다. `/mcp`는 OpenAPI 밖이고 라우트 표(`httpapi.Routes`)에도 없으므로 경로 대조의 예외 목록에 올릴 것이 없다.
- **`kg_whoami`.** 계정, 역할, 토큰 ID와 만료일(개발용 인증기의 토큰은 만료일이 없어 비운다), 한도, 서버와 모델의 버전을 답한다. 한도는 아직 없으므로 `limits: {enforced: false, note}`로 사실대로 답한다. 결과는 `{meta, next, data}`이다.
- **호출자의 글의 정리.** `kgread.CleanText`가 제어 문자, 보이지 않는 문자, 방향 제어 문자, 태그 문자를 지우고 길이를 자른다. 모르는 툴 이름, 오류 결과의 `params`, 읽기 기록의 `clientInfo`가 이것을 거친다.
- **읽기 기록과 판별 요청 수.** 툴 호출마다 로그 한 줄(`mcp_read`)을 남긴다. 판별 요청 수와 판 헤더가 없어 거절한 수는 프로세스 안의 숫자로 세고 `kg_whoami`의 `server.requests_by_version`으로 보인다(지금 이 저장소에는 지표 체계가 없어 가장 작은 방법을 골랐다). 거절한 수는 판 헤더 없음, 지원하지 않는 판, 헤더와 `_meta` 불일치로 나눈다.

## 이유

가장 큰 불확실성인 판 처리를 먼저 끝내야 다음 변경들이 같은 전송 위에 툴만 더한다. 인증과 판단은 `kgread.Gate`를 그대로 써서 REST와 같은 코드이고, 어댑터가 서로 import하지 않으므로 `adapter.http`는 마운트 지점만 안다.

## 대안과 기각 사유

- 지표 체계를 새로 만든다: 이 변경의 범위를 넘고 REQ-504는 숫자 두 가지면 된다.
- `-mcp-max-sensitivity`를 지금 받는다: 쓰는 곳이 없는 플래그는 거짓 안내가 된다.
- 헤더가 지원하지 않는 판이고 `_meta`가 2026-07-28인 요청을 -32022로 답한다: 둘이 다른 것이 먼저이므로 -32020으로 하고 지원하는 판의 목록을 함께 준다.

## 결과

- 이 변경으로 완전히 구현되고 테스트가 있는 요구사항(REQ-504, 505, 506, 515, 518)은 "예정"을 뗀다. 일부만 된 것(REQ-500, 501, 502, 503, 507, 508, 510, 514)은 예정으로 두고 kg-mcp.md 11절에 끝난 것과 남은 것을 적는다.
- 넣지 않은 것: `kg_catalog`와 KG를 읽는 툴, 계정별 한도와 동시 계산 상한, 영향 범위의 순서 변경, OAuth·OIDC, SSE, 자원·프롬프트·구독, 운영 MCP, 게이트웨이.
- 위험: 권고 값으로 둔 여섯 칸은 확인하지 못했다.
