---
title: ADR-0024 REST 계약이 기계가 지키는 문서가 된다
kind: decision
status: active
enforced_by:
  - API001
  - API020
  - API030
  - API031
  - API032
  - API040
  - API050
  - API091
  - harness/src/checks/api-lint.test.ts
  - harness/src/checks/api-assembled.test.ts
  - harness/src/checks/api-breaking.test.ts
  - harness/src/checks/api-routes.test.ts
  - harness/src/checks/api-server.test.ts
  - harness/src/checks/api-contract.test.ts
updates_docs:
  - AGENTS.md
  - ARCHITECTURE.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - biome.json
  - harness/**
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
  - docs/design-docs/adr/0019-harness-reads-go-backend-place-and-checks.md
amends_tests:
  - go:backend/cmd/praxium::TestApiServeRefusesUnsafeOrBrokenConfiguration
  - go:backend/cmd/praxium::TestApiServeWithoutATokenServesOnlyAnonymousRoutes
  - harness/src/checks/api-lint.test.ts::=MUTATIONS
  - harness/src/test-support/api-fixture.ts::=API_CONFIG
test_change_reason: 닫힌 쪽이 기본이 되어(`api serve`는 진짜 인증기가 없으면 운영 모드를 거부하고, 로그인 없이 부를 수 있는 길은 허용 목록에 있는 것뿐이다) 개발 서버를 `-mode development`로 띄우도록 테스트를 고치고, 허용 목록의 린트 테스트와 설정 샘플을 더한다. 아직 합쳐지지 않은 이 변경이 처음 잠근 테스트들이다.
amends:
  - adr: "0016"
    what: 계약의 자리와 검사, 요구사항 REQ-143, REQ-148, REQ-229의 나눔
  - adr: "0019"
    what: 코어 단위 `core.kernel`이 운영체제의 `os`를 import한다(`entropy.go` 한 파일에서 환경 변수를 읽는 입구)
decision_hash: ee9ba83fca08db6f
amended_by:
  - adr: "0028"
    what: "`core.kernel`이 `os`를 import하는 범위"
---
# ADR-0024 REST 계약이 기계가 지키는 문서가 된다

## 맥락
ADR-0016은 계약(OpenAPI)이 먼저라고 정했고 `rest-api.md`는 그 규칙을 글로 적었다. 그러나 계약 문서는 아직 없었고, 글의 규칙을 지키는 것은 사람의 눈이었다. 서버와 CLI를 짓기 전에 계약을 기계가 지키는 문서로 만든다.

## 결정
1. **계약은 `core.contract`에 조각으로 둔다.** `backend/internal/contract/openapi/parts/*.json`을 이름 순서로 합쳐 `openapi.json`을 만들고 `go:embed`로 내장한다. 합치기는 결정적이고(`contract assemble`) 같은 키가 두 조각에 있으면 실패한다. 합친 파일은 커밋하며 `API020`~`API022`가 조각과 같은지 본다.
2. **아직 없는 길은 등록부에 둔다.** `planned/routes.json`이 길마다 단계(알파 1, 알파 2, 클로즈드 베타, 오픈 베타)와 역할을 적고, `rest-api-feature-routes.md`의 표와 구현된 길이 서로 어긋나면 `API040`~`API045`로 실패한다. 구현된 길은 24개, 등록부는 173개다.
3. **스키마는 부분집합만 쓴다.** `$ref, type, properties, required, additionalProperties, items, enum, const, pattern, minLength, maxLength, minimum, maximum, minItems, maxItems, format, description, title, example, default, deprecated`와 무시되는 `x-*`. `allOf`, `oneOf`, `anyOf`는 쓰지 않는다. Go의 `contract.SchemaKeywords()`와 `api-contract.yaml`의 목록이 같아야 한다(`API091`). 서버 테스트는 같은 검증기로 실제 응답을 계약에 대 본다.
4. **확장.** `x-required-role`(어느 하나면 된다), `x-safe`, `x-long-running`. 문제 코드는 `ProblemCode` 열거와 `x-catalogue`(코드의 묶음과 상태)로 계약에 든다.
5. **린트**(`API001`~`API018`)가 `rest-api.md`의 규칙을 읽는다. 하네스의 검사 이름 목록은 `common.test.ts`가 잠그고 있어서 새 이름을 만들지 않고 `architecture` 안에서 돈다.
6. **호환성을 호환성을 깨는 변경**은 기준 ref의 계약과 견줘 길, 역할, 매개변수, 응답, 필드, 문제 코드가 빠지거나 좁아지는 것이다(`API030`). 선언은 새 확정된 ADR의 머리말 `breaks_contract`(키 목록)와 `contract_break_reason`(40자 이상)이다. 테스트의 `amends_tests`와 같은 뜻의 길이다. 깨짐이 아닌 선언은 낡은 것으로 실패한다(`API032`).
7. **서버 골격**(`adapter.http`)은 `net/http`이고 길 24개를 낸다. 엔벨로프와 `result_complete`, 문제 상세 본문, 세대가 든 커서(모양이 틀리면 `invalid_input.cursor_invalid`, 다른 세대면 `conflict.generation_changed`), ETag, 추적 ID(`traceparent`), 이름과 옛 이름으로의 단건 읽기, 단건의 `meta.allowed_actions`(고치기만)와 모델 해시를 낸다. `/v1/contract`는 엔벨로프 없이 계약 그대로 낸다. `API050`~`API052`가 서버의 길이 계약과 같은지 본다.
8. **권한은 닫힌 쪽이 기본이다.** 권한은 포트다(`core.access`의 `Authenticator`, `Authorizer`, 역할표). 개발용 인증기(`adapter.devauth`)는 고정 토큰 하나를 받고 운영 모드, 16자 미만 토큰, 모르는 역할을 거부한다. 규칙은 셋이다. (1) `api serve`는 진짜(개발용이 아닌) 인증기가 설정되지 않으면 `-mode production`(기본값)과 모르는 모드를 시작 전에 거부한다. 지금은 진짜 인증기가 없으니 운영 모드는 늘 거부하고, 0이 아닌 종료 코드와 이유를 낸다. (2) 개발 모드에서 토큰이 없으면 서버는 뜨지만 인증기가 없어서 로그인이 필요한 길은 모두 `401`(`auth.token_missing` 또는 `auth.token_invalid`)이다. 데이터는 로그인 없이 나가지 않는다. (3) 로그인 없이 부를 수 있는 길은 `harness/config/api-contract.yaml`의 `anonymous_operations` 허용 목록에 있는 것뿐이다(상태 확인 셋과 `GET /v1/problems/{code}`, rest-api의 "로그인 없이"). 목록 밖의 익명 길과 익명이 아닌 길을 적은 목록은 `API016`이고, 서버 테스트가 계약의 모든 길을 인증기 없는 서버에 대 본다. 부채 TD-0010이 OIDC로 바꿀 때까지의 임시다.
9. **CLI**(`adapter.cli`)는 같은 바이너리에 있고 공개된 `/v1`만 부른다. 종료 코드는 문제의 묶음에서 정한다(0~12). 클라이언트 플래그(`--endpoint`, `--token`, `--config`, `--json`)는 명령 앞에 둔다.
10. **구조 계약.** 단위 `core.access`, `adapter.devauth`, `adapter.http`, `adapter.cli`를 더하고 `cmd.praxium`이 `adapter.*`를 import하게 했다. `net`과 `net/http`는 `adapter.http`(와 `net/http`는 `adapter.cli`)에 허용했다. `core.kernel`에 `os`를 허용했다: `entropy.go`가 `LookupEnv`로 환경을 읽는 하나뿐인 입구가 되어, 명령줄이 환경을 읽는 경로가 `time.Now`와 같은 곳에 모인다(`GOARCH052`는 그대로다).
11. **요구사항**은 아래 표와 같이 나눴다.

## 한계
- 스키마 부분집합 밖의 JSON Schema를 쓰면 린트가 막는다. 필요하면 부분집합을 넓히는 결정이 먼저다.
- 응답의 깨짐은 필드의 있고 없음, 타입, 열거 값, 필수 여부만 본다. 의미의 바뀜은 보지 못한다.
- 서버 적합은 길과 응답의 모양까지다. 시나리오 테스트는 서버의 테스트가 맡는다.
- 개발용 인증기는 한 토큰, 한 주체다.

## 요구사항 표의 변화
- **예정을 풀었다**: REQ-143(계약, 린트, 호환성을 호환성을 깨는 변경, 경로 등록부, 서버 적합), REQ-148(오류, 엔벨로프, 페이지네이션, 조건부 읽기, 추적 ID), REQ-229(목록, `allowed_actions`, 권한에 맞게 거른 목록), REQ-241(옛 이름을 별명으로 받는다).
- **나눴다**: REQ-143에서 REQ-242(화면은 계약으로만, 예정), REQ-148에서 REQ-243(작업, 멱등성 보장, 낙관적 동시성 제어, 한도, 예정), REQ-229에서 REQ-244(`dry_run`, 그리기 좋은 꼴, SSE, 예정), REQ-147에서 REQ-245(명령줄이 공개된 `/v1`만 쓴다, 알파 1의 몫, 풀었다).
- REQ-147과 REQ-231은 예정으로 남는다.

## 이유
- 글의 규칙은 어긋난다. 규칙을 기계가 읽는 계약으로 옮기면 서버, 클라이언트, 화면의 계약이 한 곳이다.
- 합친 파일을 커밋하면 리뷰에서 계약의 바뀜이 그대로 보이고, 조각으로 두면 한 길의 바뀜이 작은 파일에서 일어난다.
- 호환성을 호환성을 깨는 변경을 ADR로 선언하게 하면 테스트 변경과 같은 무게로 남고 이유가 기록된다.

## 대안과 버린 이유
- **Go 코드에서 계약을 만들기**: 계약이 먼저라는 원칙에 거꾸로이고 화면 쪽이 읽을 문서가 코드 안에 갇힌다.
- **`oneOf`, `allOf`까지 쓰기**: 검증기와 깨짐 검사가 커진다. 알파에는 부분집합으로 충분하다.
- **API 검사를 새 검사 이름으로 만들기**: 이름 목록이 잠겨 있다.

## 결과
- 길을 더하거나 바꾸는 변경은 조각, 합친 파일, 서버, 등록부를 함께 고친다.
- 계약을 호환성을 호환성을 깨는 변경은 새 ADR의 `breaks_contract`가 필요하다.
- 부채 TD-0010(개발용 인증기)을 연다.
