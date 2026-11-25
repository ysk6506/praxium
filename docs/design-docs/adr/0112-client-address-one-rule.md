---
title: ADR-0112 호출자 주소는 한 규칙으로 — X-Forwarded-For의 모든 줄의 마지막 항목, 없으면 연결 주소, IPv6는 /64로 세기, 그리고 ADR-0109의 문서 바로잡기
kind: decision
status: active
enforced_by:
  - backend/internal/kernel/clientaddr_test.go
  - backend/internal/httpapi/xff_lines_test.go
  - backend/internal/mcpapi/xff_lines_test.go
  - backend/internal/kgread/xff_gate_test.go
  - backend/internal/hookin/hka_addr_test.go
  - backend/internal/httpapi/identity_test.go
  - backend/internal/mcpapi/more_test.go
  - TLOCK001
  - GOARCH010
touches_protected:
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0044-built-in-authentication-with-access-tokens.md
  - docs/design-docs/adr/0107-receiving-webhooks-entrance-receive-only-keys-and-limits.md
  - docs/design-docs/adr/0109-http-source-json-lines-and-arrays-inside-records.md
  - harness/config/test-lock.json
amends_tests:
  - go:backend/internal/hookin::TestHkaTheForwardedAddressIsTheLastItemOfAllTheLinesAndNothingElseCounts
test_change_reason: 입구가 주소를 읽지 못한 호출을 프록시 주소가 아닌 연결 주소의 묶음으로 세므로 로그의 묶음 이름이 none에서 연결 주소로 바뀐다. 거절과 통과의 뜻은 그대로이고 허용 목록 단언은 바꾸지 않았다.
updates_docs:
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-ingestion.md
  - docs/guides/operate-the-server.md
amends:
  - adr: "0044"
    what: 프록시 뒤 호출자 주소는 모든 X-Forwarded-For 줄의 마지막 항목이고, 실패 제한의 키는 IPv6를 /64로 센다
  - adr: "0107"
    what: 프록시 뒤에서 주소를 적지 않은 호출은 허용 목록 웹훅이 거절하고, 그 호출의 묶음은 연결 주소의 묶음이다
  - adr: "0109"
    what: 알려진 한계 두 문장의 바로잡기와 REQ-909의 파일 쪽 규칙
decision_hash: cfa6058df0aa8a02
---
# ADR-0112 호출자 주소는 한 규칙으로

## 맥락

프록시 뒤에서 호출자의 주소를 읽는 규칙이 세 곳에 따로 있었다.

- 받는 웹훅의 입구(`hookin.addrOf`, ADR-0107)는 `X-Forwarded-For`의 **모든 줄**을 쉼표로 이어 마지막 항목을 읽는다. 독립 검토를 거친 규칙이다.
- REST(`httpapi`의 `clientKey`, ADR-0044)는 `Header.Get`으로 **첫 줄만** 읽는다.
- MCP(`mcpapi`의 `clientKey`, ADR-0068)도 첫 줄만 읽는다.

프록시가 호출자의 줄 뒤에 자기 줄을 따로 붙이면(줄이 둘이 되면) REST와 MCP는 호출자가 쓴 값을 키로 삼는다. 그래서 호출자는 토큰 실패 제한을 남의 주소로 돌리거나 피할 수 있었다. 또 실패 제한의 키가 IPv6 주소 하나라, 같은 /64 안에서 주소를 바꿔 가며 제한을 피할 수 있었다.

또 ADR-0109의 "알려진 한계" 두 문장이 지금의 구현과 맞지 않아 이 결정에서 함께 바로잡는다.

## 결정

1. **한 구현.** `backend/internal/kernel/clientaddr.go`에 세 함수를 둔다. 표준 라이브러리(`net/netip`, `strings`)만 쓰고, 요청이 아니라 글자(연결 주소의 글자와 헤더 줄의 목록)를 받는다. kernel은 네트워크를 모른다.
   - `ClientAddr(remote, forwarded, trustProxy) (addr, vouched)`: 프록시를 믿으면 `forwarded`의 모든 줄을 쉼표로 이은 목록의 **마지막 항목**이 주소일 때 그것이고 `vouched`는 참이다. 헤더가 없거나 마지막 항목이 주소가 아니면(예: `unknown`, 빈 항목) 연결 주소이고 `vouched`는 거짓이다. 프록시를 믿지 않으면 연결 주소이고 `vouched`는 주소가 읽혔는지다. 주소 읽기는 포트와 대괄호를 떼고, `::ffff:`로 시작하는 IPv4를 풀고, 존을 지운다. 어느 것도 읽히지 않으면 0값과 거짓이다.
   - `AddrGroup(a)`: IPv4는 그대로, IPv6는 /64, 0값은 `none`.
   - `CallerKey(remote, forwarded, trustProxy)`: `ClientAddr`의 주소가 있으면 그 글자, 없으면 연결 주소의 호스트 부분.
2. **세 곳이 같이 쓴다.** `hookin`의 `addrOf`와 `addrGroup`은 kernel을 부른다. REST의 `clientKey`와 MCP의 `clientKey`는 `kernel.CallerKey`를 부른다. MCP의 `hostOfAddr`는 지운다.
3. **웹훅 입구의 허용 주소 목록.** `vouched`가 거짓인 호출은 허용 목록이 있는 웹훅에서 거절한다(주소가 없는 호출을 거절하던 옛 규칙과 같다). 세는 묶음은 연결 주소의 묶음이다.
4. **토큰 실패 제한의 키.** 제한(`Blocked`, `Failed`)은 키 글자를 주소로 읽어 `kernel.AddrGroup`을 쓴 값을 쓴다(읽히지 않으면 키 그대로). 감사 줄의 `remote`에는 온전한 키를 쓴다. 한 /64 안의 주소들은 실패를 함께 세고 감사 줄에는 각자의 주소가 남는다.
5. **운영 문서.** 프록시 단락에 규칙을 적는다. 허용 주소 목록에는 호출자의 주소만 넣고 프록시 자신의 주소는 넣지 않는다. 프록시를 거친 호출은 프록시의 주소로 세지 않으며(마지막 항목이 호출자의 주소다), 목록에 프록시 주소를 넣어도 뜻이 없다.

## 잠긴 테스트의 변경

- `mcpapi::TestCallerAddressForTheLimiter`와 `httpapi::TestBehindAProxyTheLastForwardedAddressIsTheCaller`는 **단언이 그대로** 통과한다. 두 표는 이 규칙을 만족하도록 짰다.
- `hookin::TestHkaTheForwardedAddressIsTheLastItemOfAllTheLinesAndNothingElseCounts`는 로그에 쓰는 묶음 이름의 단언만 바뀌었다. 주소를 읽지 못한 호출은 옛 `none`이 아니라 연결 주소의 묶음으로 센다. 거절과 통과(401, 202)와 허용 목록의 단언은 그대로다.

## 이유

- 첫 줄을 읽으면 호출자가 쓴 값이 키가 된다. 마지막 항목은 프록시가 덧붙인 값이라 호출자가 지어낼 수 없다. 입구가 이미 쓰던 규칙이므로 셋이 같아진다.
- 주소를 읽지 못한 호출은 허용 목록이 있는 웹훅에서 어떤 묶음이든 거절된다. 묶음은 실패 기록과 동시 처리 한도를 나누는 데만 쓰인다. 그래서 `none` 하나로 묶으면 주소를 적지 않은 모든 호출이 한 묶음이 되고, 연결 주소로 묶으면 그 연결(보통 프록시)마다 나뉘어 로그에서 어느 연결인지 보인다. 거절과 통과의 뜻은 두 경우가 같다.
- IPv6를 /64로 세는 것은 입구(ADR-0107)가 이미 정한 규칙이다. 한 고객의 네트워크 안에서 주소를 바꿔 가며 제한을 피하는 것을 막는다.

## 대안과 기각 사유

- **REST와 MCP만 첫 줄 대신 마지막 줄로 고치는 것.** 입구와 규칙이 다시 갈라진다. 기각.
- **`hookin`에 주소 규칙을 두고 나머지가 부르는 것.** 어댑터끼리 서로 가져다 쓰면 의존 방향이 어긋난다(`httpapi`와 `mcpapi`는 `hookin`을 가져올 수 없다). kernel은 모든 유닛이 읽을 수 있다. 채택하지 않음 — kernel에 둔다.
- **`ClientAddr`가 `*http.Request`를 받는 것.** kernel이 `net/http`에 닿게 된다. 기각.
- **주소를 읽지 못한 프록시 뒤 호출을 허용 목록과 무관하게 통과시키는 것.** 허용 목록이 프록시 뒤에서 무력해진다. 기각.

## 결과

- 호출자가 자기 줄을 보내도 키는 프록시가 쓴 마지막 항목이다. REST와 MCP의 실패 제한이 입구와 같은 규칙으로 센다.
- IPv6 한 /64의 실패가 한 묶음이 되고, 감사 줄에는 온전한 주소가 남는다.
- 운영 문서(`docs/guides/operate-the-server.md`)와 요구사항(REQ-314, REQ-909)의 문장이 이 규칙과 맞는다.
- 실제 주소가 아닌 값을 주는 프록시는 허용 목록 웹훅에서 주소 없는 호출로 거절된다(설정 오류가 드러나는 쪽으로 실패한다).

## ADR-0109 문서 바로잡기

ADR-0109의 "알려진 한계"는 확정된 결정의 본문이라 고치지 않고(DOC070), 이 결정에서 다음을 바로잡는다.

1. "어댑터 흐름의 파일 JSON Lines에 `records_path`를 더하면 … 실행 때에야 드러난다"는 **틀리다**. `[*]`나 `^`처럼 이 문법으로 읽지 않는 경로는 정의 때 `source.path.unsupported` 오류다(예: `a[*].b`). 옛 점 나누기와 읽는 법이 다를 수 있다는 앞부분만 맞다. 어댑터 흐름에 이미 저장된 파일 JSON Lines 정의 가운데 `a[*].b`나 `^.x` 경로를 쓴 것은 이제 정의 검사에서 거절된다. 예전에는 그런 이름의 키로 조용히 읽혔다. 의도한 방향이다.
2. 빈 파일 JSON Lines가 완전인 이유는 "잠긴 테스트가 단언한다"가 아니다. **옛 규칙과 같게 둔다(파일은 끝까지 읽혔다)** 가 이유다. `TestStreamPagesOfAnEmptyDatasetIsOneLastPage`는 `records_path`가 없는 빈 JSON Lines와 CSV만 단언한다.
3. REQ-909(행의 바이트 합 상한)에 파일 기준을 더한다. 파일의 JSON Lines(어댑터 흐름, `records_path`)에는 응답 한도가 없으므로 지금까지 읽은 바이트의 4배에 고정 몫 8 MiB를 더한 값까지이고, 넘으면 그 데이터셋은 불완전이다(보인 것만이 아니다. ADR-0109 7번과 N1).

## 확인 기록

- 테스트: `backend/internal/kernel/clientaddr_test.go`(`TestCad...`), `backend/internal/httpapi/xff_lines_test.go`, `backend/internal/mcpapi/xff_lines_test.go`(두 줄에서 마지막 항목), `backend/internal/kgread/xff_gate_test.go`(한 /64의 실패, 감사 줄의 온전한 주소).
- 고치기 전에 두 줄 테스트(REST, MCP)가 `198.51.100.1`을 키로 삼아 실패하는 것을 확인했다.
