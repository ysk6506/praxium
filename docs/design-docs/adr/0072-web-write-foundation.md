---
title: ADR-0072 화면의 쓰기 기반
kind: decision
status: active
enforced_by:
  - WEB010
  - WEB011
  - WEB012
  - WEB013
  - WEB001
  - WEB043
  - WEB045
  - ARCH052
  - apps/web/src/api/headers.test.ts
  - apps/web/src/data/headers.test.tsx
  - apps/web/src/data/mutation.test.tsx
  - apps/web/src/data/poll.test.tsx
  - apps/web/src/data/json-doc.test.ts
  - apps/web/src/data/leaveGuard.test.tsx
  - apps/web/src/data/idempotency.test.ts
  - apps/web/src/ui/form.test.tsx
  - apps/web/src/settings-screen.test.tsx
updates_docs:
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-web-cli-release.md
  - docs/design-docs/web-ui-rules.md
  - ARCHITECTURE.md
touches_protected:
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-web-cli-release.md
  - harness/config/test-lock.json
  - harness/config/architecture.yaml
  - harness/config/web.yaml
amends: []
amends_tests:
  - apps/web/src/api/generator/generator.test.ts::[REQ-276] the generator turns operations into functions > writes path, query and body into the arguments and keeps header parameters out
  - apps/web/src/test-support/fake-api.ts::=Asked
  - apps/web/src/test-support/fake-api.ts::=Reply
  - apps/web/src/test-support/fake-api.ts::=fakeApi
test_change_reason: 생성기는 계약의 머리글 인자를 버렸고 잠긴 테스트가 그 버림을 단언했다. 쓰기 화면은 Idempotency-Key와 If-Match를 보내야 하므로 머리글 인자를 호출 함수의 인자로 만들도록 바꾼다. 그 테스트는 같은 문서로 같은 연산을 만들되 머리글 인자가 headers 인자에 있다는 단언으로 뒤집고 이름을 고쳤으며, 나머지 단언은 그대로 둔다. 가짜 API(test-support/fake-api.ts)의 지원 코드 셋(Asked, Reply, fakeApi)은 요청의 머리글을 기록하고 응답에 머리글을 줄 수 있게 필드만 더했고 기존 필드와 동작은 그대로다.
decision_hash: bbd0ad7f529bcb8e
---
# ADR-0072 화면의 쓰기 기반

## 맥락

웹 화면에는 서버에 저장하는 폼이 없다. 뒤의 변경이 소스와 매핑을 편집하고 수집을 실행하는 화면을 만든다. 그 화면들이 같은 기반 위에 서도록, 이 변경은 기반만 만든다. 새 화면과 REST 계약의 변경은 없다.

## 결정

1. **머리글.** 생성기가 계약의 머리글 인자(`Idempotency-Key`, `If-Match`, `If-None-Match`)를 호출 함수의 `headers` 인자로 만든다. 요청 한 곳(`client.ts`)이 싣되 `Authorization`, `Accept`, `Content-Type`은 바꾸지 못한다. 본문은 JSON만 보낸다.
2. **`ETag`는 `withEtag(client, (c) => 호출)`로 읽는다.** 생성 함수의 결과 타입은 그대로다. 요청에 `onResponse` 귀를 달아 마지막 응답의 `ETag`를 모은다. `Requester`에 멤버를 더하면 그것을 직접 구현한 테스트가 깨지고, 결과 타입을 `{data, etag}`로 바꾸면 모든 호출자가 깨진다.
3. **변경용 훅 `useMutation`.** 진행 중에는 다시 내지 않고, 제출 하나에 멱등 키 하나를 쓴다(같은 제출의 다시 시도는 같은 키, 내용이 바뀌면 새 키, 성공 뒤에도 새 키, `conflict.idempotency_key_reused`면 버린다). 길이는 서버와 같은 8~128자다. 키는 `newKey`로 만들고 기본값은 `newIdempotencyKey`(결정 8)이며, 테스트는 `newKey`를 넘겨 결정적으로 돈다. 실패는 `describeError`의 문장과 `errors[]`의 필드별 오류로 나온다. 428, 412, 409(요청 번호의 충돌과 처리 중)의 문구를 더했고 412의 `current_etag`는 `currentEtag`로 읽는다.
4. **쓴 뒤 다시 읽기는 주제 신호 하나다(`invalidation.ts`).** 읽기가 `refreshOn`에 주제를 적고, 쓰기가 `invalidates`에 바꾼 주제를 적으면 그 읽기만 로딩 표시 없이 다시 읽는다. 답을 담아 두는 곳이 없다. 캐시 라이브러리는 들이지 않았다: 답을 나눠 쓸 화면이 아직 없고, 의존성 하나(`WEB001`)와 그 규칙을 배우는 값이 이 화면의 크기에 맞지 않는다. 같은 읽기를 여러 화면이 나눠 쓰는 일이 생기면 그때 다시 정한다.
5. **읽기 훅.** `refresh()`(로딩 표시 없이)와 `poll`(간격, 멈춤 조건 `until`, 숨겨진 탭은 쉰다, 실패하면 멈춘다)을 더했다. 기존 호출자의 동작은 같다.
6. **JSON 문서 도구(`json-doc.ts`).** JSON 포인터(RFC 6901)로 읽고 쓰고 지우고 목록 항목을 옮기는 순수 함수와, 서버의 오류 위치를 폼의 칸에 잇는 `attachIssues`다. 속성 테스트로 쓰고 읽으면 같다, 건드리지 않은 곳은 그대로다, 쓰고 지우면 원래 문서다를 본다.
7. **폼 부품(`ui`).** 한 줄, 여러 줄, 숫자, 시크릿 입력(`TextField`, `TextAreaField`, `NumberInput`, `SecretField`), `ListEditor`, `ConfirmDialog`(Base UI의 `AlertDialog`), `ResultNotice`(`aria-live`), `Form`과 `SubmitButton`이다. 떠날 때의 경고는 `data`의 `useLeaveGuard`다. 접속 설정의 토큰 폼이 이를 쓰고 제출 단추를 미리 비활성화하지 않는다.
8. **난수가 들어오는 문은 한 파일이다(`data/idempotency.ts`).** 멱등 키는 탭마다, 사람마다 겹치지 않아야 하므로 브라우저의 암호용 난수가 필요하다. `apps/web`은 `ARCH052`가 난수 호출을 막으므로, 화면마다 키 만드는 함수를 넘기게 하는 대신 난수를 읽는 파일 하나를 구조 계약의 `nondeterminism_allowed`에 더했다. Go 백엔드의 `kernel/entropy.go`, 코어의 `entropy.ts`와 같은 설계로, 난수가 들어오는 문을 한 곳으로 정한 것이다. 이는 검사를 느슨하게 하는 것이 아니라 허용 범위를 한 파일로 좁히는 것이다. 이 파일은 키를 만드는 일만 한다: `crypto.randomUUID()`로 UUID를 만들고, 그것이 없으면 `crypto.getRandomValues()`로 UUID(v4)를 만들며, 둘 다 없으면 오류를 던진다(약한 난수로 내려가면 키가 겹쳐 서버가 남의 답을 다시 줄 수 있다). 다른 파일은 난수를 읽지 못하고 이 함수나 `newKey`를 받는다.
9. **`web.yaml`의 낡은 주석.** `WEB045` 줄의 "Today: JS 143 KB"를 지금 측정한 값으로 고쳤다. 주석의 측정값만 고쳤고 예산의 숫자는 그대로다.

## 이유

화면마다 머리글, 중복 제출, 오류 위치, 포커스를 따로 짜면 화면마다 어긋나고 접근성 규칙(REQ-409)이 화면 수만큼 되풀이된다. 기반을 먼저 한 번 만들고 테스트로 고정한다. 숫자 입력은 Base UI의 `NumberField`가 아니라 브라우저의 `<input type="number">`다. 단추와 끌기가 필요 없고, `NumberField`는 gzip으로 약 4.9KB를 더한다.

## 번들의 측정

`WEB045`의 예산(JS 170000, CSS 10000)은 이 변경에서 그대로다. 측정은 `pnpm --filter @praxium/web build` 뒤 gzip 크기의 합이다.

- 변경 전은 JS 161151, CSS 6425이고, 변경 뒤는 JS 163098, CSS 6588이다(접속 설정 화면이 쓰는 부분만 번들에 든다).
- 부품을 쓰면 늘어나는 양(JS, 바이트): `ListEditor` 약 1100, `useMutation`(다시 읽기 신호와 기본 키 포함) 약 1140, `json-doc` 약 1500, `useLeaveGuard` 약 650, `ConfirmDialog` 약 300, `ResultNotice` 약 280, 나머지 100 안쪽. 전부 쓰면 약 167900이고 여유는 약 2100이다.
- 뒤의 화면 셋(화면마다 약 3000~5000)과 새 호출 약 20개(약 700)에 남은 부품(약 4700)이 더해지면 약 177500~183500으로, 예산을 약 7500~13500 넘는다고 본다.

예산의 결정은 화면을 만드는 단계의 결정 기록이 한다. 이 결정 기록은 예산을 바꾸지 않는다.

## 넣지 않은 것

새 화면, 계약의 변경, 파일 올리기, 전역 상태와 캐시 라이브러리, 예산과 검사의 변경, 화면 단위 지연 로딩(`import()`의 크기만 보았다. 모든 조각의 합을 재므로 예산을 줄이지 못한다).

## 결과

- 잠긴 테스트 하나(생성기의 머리글 단언)가 바뀌었다. 이름과 단언을 뒤집었고 나머지는 그대로다. 가짜 API(`fake-api.ts`)의 지원 코드 셋은 필드만 늘었다. 이 브랜치가 새로 만든 테스트는 고친 것이 아니라 더한 것이다.
- `ErrorKind`에 `conflict`가 늘었다. 409와 412 계열이다.
- `harness/config/architecture.yaml`의 `nondeterminism_allowed`에 파일 하나가 늘었다.
