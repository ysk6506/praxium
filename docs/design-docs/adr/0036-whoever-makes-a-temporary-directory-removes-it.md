---
title: ADR-0036 임시 디렉터리는 만든 쪽이 지운다
kind: decision
status: active
enforced_by:
  - harness/src/scratch.test.ts
updates_docs:
  - docs/design-docs/harness-guide.md
touches_protected:
  - harness/src/scratch.ts
  - harness/src/scratch.test.ts
  - harness/src/test-support/scratch-hook.ts
  - harness/src/test-support/mini-repo.ts
  - harness/src/checks/api-contract.ts
  - harness/src/checks/go-cover.ts
  - harness/config/test-lock.json
decision_hash: 995d00d35381030f
---
# ADR-0036 임시 디렉터리는 만든 쪽이 지운다

## 맥락

개발 환경의 디스크가 `/tmp`에 쌓인 임시 디렉터리로 가득 찼다. 접두사별로 `praxium-mini-`가 약 12만 개, `praxium-lic-`가 약 4천 개였고, `nowhere-`, `nogit-`, `praxium-gobin-`, `praxium-api-`, `praxium-tiny-`가 각각 100~200개였다. `pnpm check` 한 번에 하나도 지워지지 않고 `praxium-mini-` 760여 개, `praxium-lic-` 20여 개가 남는 것을 확인했다.

원인은 두 가지다.

1. 테스트 지원 코드(`miniRepo`)와 테스트 본문(`licenses.test.ts`, `common.test.ts`, `go-tools.test.ts`, `api-real-go.test.ts`)이 `mkdtempSync`로 디렉터리를 만들고 아무도 지우지 않는다. 테스트 하나가 만드는 저장소는 테스트가 끝나도 남는다.
2. 검사 코드 `api-contract.ts`의 `defaultIo`는 호출하자마자 디렉터리를 만들었다. 지우는 `dispose`를 부르지 않는 호출(git만 읽는 호출)은 빈 디렉터리를 남겼다.

잠긴 테스트의 본문은 바꿀 수 없으므로 본문에 지우는 코드를 넣는 방법은 쓸 수 없다.

## 결정

1. 원칙: 임시 디렉터리는 만든 쪽이 끝날 때 지운다. 테스트는 훅으로, 검사 코드는 `try/finally`로 지운다.
2. 한 번의 실행(검사 한 번, 테스트 파일 하나)이 만드는 모든 것은 `<tmp>/praxium-scratch/run-<pid>-<임의 문자>/` 한 디렉터리 아래에 모은다(`harness/src/scratch.ts`).
3. 테스트 파일은 지원 코드(`mini-repo.ts`)를 import하면 `scratch-hook.ts`가 함께 실행된다. 훅은 `TMPDIR`을 그 디렉터리로 바꾸므로 `os.tmpdir()`을 쓰는 코드는 테스트 본문이든 지원 코드든 시작한 프로그램(`go build`)이든 모두 그 아래에 만든다. `afterAll`이 디렉터리를 지우므로 테스트가 실패하거나 시간 초과여도 지워진다. 작업자가 중간에 멈출 때를 위해 `exit`에서도 지운다.
4. 강제 종료(`SIGKILL`)처럼 지우지 못한 경우를 위해 실행을 시작할 때 `praxium-scratch` 아래에서 마지막 수정이 하루를 넘었고 소유 프로세스가 없는 `run-*` 디렉터리를 지운다. 같은 시간에 도는 다른 실행의 디렉터리는 소유 프로세스가 살아 있으면 오래돼도 지우지 않고, 접두사가 다른 디렉터리는 건드리지 않는다.
5. 검사 코드(`go-cover.ts`, `api-contract.ts`)도 같은 `openScratch`로 디렉터리를 얻고 `finally`에서 `dispose`한다. `api-contract.ts`는 첫 빌드 때까지 디렉터리를 만들지 않는다.
6. 새 테스트(`scratch.test.ts`)가 훅의 효과, 오래된 디렉터리의 정리(살아 있는 실행과 젊은 디렉터리는 남김)를 확인하고, 테스트 파일·지원 코드가 `mkdtemp`나 `tmpdir()`을 쓰면 훅에 이르는 import가 있는지, 검사 코드가 `mkdtemp`를 직접 쓰지 않는지 검사한다.

잠긴 테스트의 본문과 단언은 바꾸지 않았다. `mini-repo.ts`에는 import 한 줄만 더했으므로 기존 잠금 항목의 지문은 그대로여서 `amends_tests`가 필요하지 않다. 새 항목은 `pnpm lock-tests`가 추가한다.

## 이유

- 만든 쪽이 지우는 것이 가장 단순하고, 누가 무엇을 남겼는지 추적하지 않아도 된다.
- 훅과 `TMPDIR` 방식은 본문을 바꾸지 않고 모든 테스트 파일을 한 번에 덮는다. 새 테스트가 `mkdtempSync`를 써도 지원 코드를 import하는 한 지워진다.
- 하루가 지난 것만 지우고 살아 있는 프로세스의 것을 남기면 동시에 도는 다른 검사 실행을 해치지 않는다.

## 대안과 버린 이유

- 테스트 본문에서 `rmSync`를 부르기: 잠긴 본문이 바뀌어 `amends_tests`가 필요하고 테스트마다 반복해야 한다.
- `vitest.config.ts`에 `globalSetup`이나 새 `setupFiles` 추가: `tests.yaml`이 금지하거나 허용 목록을 넓혀야 하는 설정 변경이다.
- 실행 뒤에 `/tmp`를 접두사로 일괄 삭제하기: 같은 시간에 도는 다른 사람의 실행분을 지울 수 있다.

## 결과

- `pnpm check`가 끝난 뒤 `praxium-scratch` 아래에 이번 실행의 디렉터리가 남지 않는다.
- 이미 쌓인 디렉터리는 이 변경이 지우지 않는다. 접두사가 옛 형식이므로 개발 환경에서 한 번 직접 지운다.
