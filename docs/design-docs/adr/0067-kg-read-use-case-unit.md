---
title: ADR-0067 KG 읽기의 공통 유닛 core.kgread를 더한다 (ADR-0062 결정 9의 구현)
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH021
  - backend/internal/kgread/service_test.go
touches_protected:
  - harness/config/architecture.yaml
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/kg-mcp.md
decision_hash: 5977425ce327b5b3
---
# ADR-0067 KG 읽기의 공통 유닛 core.kgread를 더한다

## 맥락

ADR-0062 결정 9는 인증, 읽기 필터, 오류 코드의 대응, 응답 엔벨로프의 변환, 입력의 해석을 REST와 MCP 어댑터가 함께 쓰는 코어 유닛으로 옮기기로 했다. 구조 계약은 어댑터끼리의 import를 막으므로, 두 어댑터가 같은 코드를 쓰려면 그 코드가 코어에 있어야 한다. 지금은 모두 `httpapi` 안에 있다.

## 결정

- 새 코어 유닛 `core.kgread`(`backend/internal/kgread`)를 더한다. import는 `core.kernel`, `core.model`, `core.graph`, `core.query`, `core.access`만 허용하고 어댑터와 `net`, `net/http`는 쓰지 않는다. `adapter.http`의 `may_import`에 `core.kgread`를 더한다. 바꾸는 보호 경로는 `harness/config/architecture.yaml` 하나다.
- 옮기는 원칙: HTTP에 묶인 것은 `httpapi`에 남긴다. 옮긴 것은 토큰에서 주체까지와 역할 판정(`Gate`), 읽기 필터, 질의 오류에서 오류 코드로의 대응, 엔벨로프 변환, 질문 넷의 JSON 해석과 호출(`Service`), 모델 목록의 내용이다. 남긴 것은 호출자 주소 구하기, 헤더, 쿼리 문자열, 상태 코드, ETag, 목록의 틀이다.
- 겉모양은 "주체와 원본 JSON 인자를 받아 결과 또는 오류 코드를 돌려주는" 함수다. 한 질문에 해석 코드가 두 벌 생기지 않게 REST 핸들러도 같은 함수를 부른다.
- REST의 동작은 하나를 빼고 바뀌지 않는다. 본문이 JSON 객체가 아니어서 생기는 타입 오류의 `params.expected`가 Go 타입 이름(`httpapi.findBody`)이었는데, 내부 이름이 새던 것이므로 `"object"`로 바로잡는다. 다른 필드의 `expected`는 그대로다.

## 이유

코어에 두면 어댑터가 늘어도 인증, 필터, 오류 코드의 규칙이 한 곳에만 있다. 동작을 보존하는 이동과 동작의 변경을 나누면 기존 테스트가 이동의 안전망이 된다.

## 대안과 기각 사유

- MCP 어댑터가 `httpapi`를 import한다: 구조 계약이 어댑터끼리의 import를 막고, 전송이 서로 묶인다.
- 두 어댑터에 같은 코드를 복사한다: 해석과 오류 코드가 어긋난다.

## 결과

- 이 변경에 넣지 않는다: MCP 어댑터와 툴 등록부, 계정별 한도와 동시 계산 상한, 영향 범위의 순서 변경과 필터, 새 오류 코드와 설정.
- 새 유닛의 커버리지 하한을 더한다(새 유닛 등록).
- 다음 변경부터 구조 계약을 바꾸는 구현은 새 결정 기록으로 바꾼다.
