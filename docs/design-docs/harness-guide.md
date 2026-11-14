---
title: 품질 하네스 사용법
kind: guide
status: active
covers:
  - harness/src/run-all.ts
---
# 품질 하네스

`harness/`는 리포지토리의 품질이 오래 유지되도록 지키는 검사 모음이다. 각 검사는 자신의 메타 테스트(`harness/src/checks/*.test.ts`)로 "정말 위반을 잡는가"가 검증되어 있다. 테라리움의 하네스를 가져온 것이다(ADR-0002).

| 요구 | 검사 | 코드 |
|---|---|---|
| 문서 품질 (REQ-020) | `harness/src/checks/docs.ts` | `DOC*` |
| 아키텍처 강제 (REQ-021) | `harness/src/checks/architecture.ts` | `ARCH*` |
| 제3자 라이선스 (함께 나가는 npm 패키지와 자료는 허용 목록 안의 라이선스여야 한다) | `harness/src/checks/licenses.ts` | `LIC*` |
| REST 계약 (REQ-143, REQ-148) | `harness/src/checks/api-contract.ts` (`architecture` 안에서 실행된다) | `API*` |
| 웹 UI(`apps/web`)의 의존성 허용 목록, 생성한 API 코드와 계약의 일치, 색 리터럴, 백엔드의 독립, 웹 UI 규칙의 기계 검사(토큰 대비, 위험한 싱크, 외부 주소, 포커스 표시, 움직임, 번들 예산) (REQ-276, REQ-277, REQ-279, REQ-316~REQ-318) | `harness/src/checks/web.ts` (`architecture` 안에서 실행된다. 설정은 `harness/config/web.yaml`) | `WEB*` |
| 기술 부채 (REQ-022) | `harness/src/checks/debt.ts` | `DEBT*` |
| 검사 무시 금지 (무시 주석·건너뛴 테스트·CI `continue-on-error`는 이유와 근거 번호가 있어야 하고 수는 늘 수 없다) | `harness/src/checks/ignores.ts` | `IGN*` |
| 테스트 변조 방지 (REQ-023) | `harness/src/checks/testguard.ts` | `TEST*` `TCFG*` `TLOCK*` (Go 테스트 함수도) |
| 커버리지 (REQ-023) | `harness/src/checks/coverage.ts` | `COV*` |
| 요구사항 추적 (`**예정(ADR-NNNN)**` 요구는 따로 나열, Go 테스트는 `Test` 함수 위 `// [REQ-NNN]` 주석을 읽는다) | `harness/src/checks/traceability.ts` | `TRACE*` |
| 화면의 용어와 문서의 표준 용어 (한 가지 의미에 한 가지 용어, 옛 표기 금지) | `harness/src/checks/ui-words.ts`, `doc-terms.ts` | `WORD*` |
| 품질 등급표 (REQ-024, 단위별 등급 A~D, 하네스가 만드는 생성물) | `harness/src/checks/quality.ts` | `QUAL*` |
| 가드레일 보호 | `harness/src/checks/protected.ts` | `PROT*` |
| Go 코드(ADR-0019): 단위와 import, 공개 인터페이스, 시계·난수·환경, 모듈 표, 크기, 검사 무시, 테스트, 형식 | `harness/src/checks/go-*.ts` | `GOARCH*` `GODEP*` `GODEBT*` `GOIGN*` `GOTEST*` `GOCOV*` `GOTOOL*` |

문서 용어 검사(ADR-0029): `WORD003`은 용어집 표의 옛 표기가 문서에 있으면, `WORD004`는 사전을 읽지 못하면 발생한다. 설정은 `ui-words.yaml`의 `documents:`이고 `mode`가 `report`이면 개수만 알리고 `fail`이면 실패한다. 문서별 개수는 `pnpm tsx harness/src/checks/ui-words.ts --documents`, 행 단위로는 `--detail`로 확인한다. 코드, 인용, 링크 대상, 참조 문서는 건너뛴다.

설정은 `harness/config/`에 있고 보호 경로다. 변경하려면 같은 변경에 확정된 결정 기록이 있어야 한다.

## Go 코드를 읽는 검사 (ADR-0019)

하네스는 TypeScript 도구로 남고 Go 코드를 읽는다. Go 검사는 이미 있는 검사 이름 안에서 실행된다(검사 이름의 목록은 `common.test.ts`가 잠그고 있다): `architecture`에서 `GOARCH*`(단위, import, 공개 인터페이스, 시계·난수·환경, 금지 구문, 빌드 제약, 컴파일된 실행 파일), `GODEP*`(`go.mod`와 ARCHITECTURE.md의 모듈 표), `GOTOOL*`(`gofmt`, `go vet`), `debt`에서 `GODEBT*`, `ignores`에서 `GOIGN*`, `test-integrity`에서 `GOTEST002`(테스트 없는 단위), `GOTEST003`~`GOTEST006`(테스트의 정적 규칙), `coverage`에서 `GOTEST001`(`go test` 실패)과 `GOCOV*`(단위의 문장 커버리지 하한)다. 구조 계약은 `harness/config/architecture.yaml`의 `go:` 절이고, 한도와 대장은 TypeScript와 같은 `debt.yaml`과 대장을 쓴다.

- import는 `go list`가 아니라 소스에서 읽는다(`go-lex.ts`). 테스트 파일, 빌드 태그가 붙은 파일, 별칭, 행 번호를 한 번에 확인하려는 것이다. 컴파일러가 아는 것(타입 오류, 없는 패키지)은 `go vet`이 본다.
- Go가 설치돼 있어야 한다(`GOTOOL000`). 하네스는 `GOTOOLCHAIN=local`로 다른 Go를 내려받지 않고, 모듈 파일을 수정하지 않는다.
- 커버리지 하한은 `harness/config/coverage-floor.json`에 `go:<단위>`로 있고, `pnpm ratchet`이 올린다. 하한이 `ratchet`이 정할 값보다 1점 넘게 낮으면 `COV006`(TypeScript)과 `GOCOV003`(Go)로 실패한다: 높인 커버리지는 곧바로 고정한다.
- import할 수 있는 단위는 `may_import`(제품 코드와 테스트 파일)와 `test_may_import`(`_test.go`만 추가로)로 구분해 적는다. 제품 코드가 테스트 전용 단위를 import하면 `GOARCH010`이고(`GOARCH005`는 두 목록에 같은 이름이 있을 때, `GOARCH004`는 없는 단위일 때), 새 단위에서 "테스트에서만 쓴다"고 적은 허용은 `test_may_import`에 둔다.
- `restricted_stdlib`의 항목이 `.go`로 끝나면 그 파일 하나다(없는 파일이나 단위는 `GOARCH006`). `os`는 `cmd.praxium`과 `backend/internal/kernel/entropy.go`만 import한다.
- import 경로는 보이는 그대로 읽는다: 이스케이프나 사용하지 않는 문자가 포함된 경로는 `GOARCH054`다. 테스트 파일의 빌드 제약(`//go:build`, `// +build`, OS·아키텍처 접미사)은 `GOARCH055`, 커밋될 수 있는 위치의 컴파일된 실행 파일과 `*.test`는 `GOARCH056`이다(`.gitignore`가 차단한다).
- 테스트의 정적 규칙(TypeScript의 `TEST003`~`TEST005`에 해당): `GOTEST004`는 상수로 결정되는 조건(`if 1+1 != 2`, `if x != x`), `GOTEST005`는 에러 검사 뒤에 실패 처리 없이 `return`·`continue`·`break`하거나 실패할 경로가 없는 `recover()`, `GOTEST006`은 `m.Run`을 호출하지 않는 `TestMain`과 `test.run` 같은 플래그를 설정하는 코드다. 이름으로 참조하는 상수(`const want = 3`)는 해석하지 않는다.
- 테스트 잠금, 요구사항 추적, 품질 등급표도 Go를 읽는다(ADR-0023, 부채 TD-0008을 닫았다). 아래 절에 있다.

## 웹 UI 검사 (ADR-0035)

`architecture` 안에서 도는 `WEB*` 검사가 `apps/web`을 지킨다. 검사 이름 목록은 늘리지 않았다. `WEB001`은 `apps/web/package.json`의 의존성이 허용 목록에 있는지, `WEB010`~`WEB013`은 생성한 API 코드의 계약 해시와 본문 해시와 생성기를 다시 돌린 결과, `WEB020`·`WEB021`은 토큰 파일 밖의 색, `WEB030`~`WEB032`는 백엔드가 `apps/web` 없이 빌드되는지 본다. `WEB040`~`WEB045`는 [웹 UI 규칙](web-ui-rules.md)의 기계 검사다: 토큰의 글자와 테두리 대비(쌍은 `web.yaml`의 `rules.contrast`), 위험한 싱크, 절대 주소, 포커스 표시 제거, 움직임 줄이기, 번들 예산(`pnpm tsx harness/src/checks/web.ts --budget`, CI가 웹 빌드 뒤에 돌린다). 마지막 것은 빌드 결과가 없으면 `pnpm check`에서는 건너뛴다. Base UI를 `ui` 계층에서만 import하는 규칙은 `ARCH020`이다. `tests.yaml`의 `unlocked_globs`(화면 `apps/web/src/screens/**`)에 든 테스트 파일은 잠금과 요구사항 추적에서 빠지고 정적 테스트 규칙은 그대로 받는다.

## REST 계약 검사 (ADR-0024)

`architecture` 안에서 도는 `API*` 검사가 `backend/internal/contract`의 OpenAPI 계약을 지킨다. 설정은 `harness/config/api-contract.yaml`이다. 검사 이름 목록은 늘리지 않았다.

- **린트** (`API001`~`API018`): `rest-api.md`의 규칙을 문서에서 읽는다. 경로 이름, 동사, 엔벨로프, 오류 응답, `x-required-role`, 목록의 페이지네이션 매개변수, 예시가 스키마를 준수하는지 같은 것이다.
- **병합 결과 = 조각** (`API020`~`API022`): Go가 병합한 결과가 커밋된 `openapi.json`과 같아야 한다.
- **호환성을 깨는 변경** (`API030`~`API032`): 기준 ref의 계약과 비교해 경로, 역할, 매개변수, 응답, 필드, 문제 코드가 빠지거나 좁아지면 실패한다. 새로 확정된 ADR의 머리말 `breaks_contract`(키 목록)와 `contract_break_reason`(40자 이상)으로 선언한다. 선언한 변경이 실제로는 호환성을 깨지 않으면 무효한 선언이다(`API032`).
- **경로 등록부** (`API040`~`API045`): 아직 구현되지 않은 경로(`planned/routes.json`)와 `rest-api-feature-routes.md`의 표, 구현된 경로가 서로 불일치하지 않아야 한다.
- **서버 적합** (`API050`~`API052`): 서버가 등록한 경로(`api routes --json`)가 계약의 구현된 경로와 같아야 한다.
- **스키마 부분집합** (`API091`): Go의 `SchemaKeywords()`와 설정의 목록이 같아야 한다.

Go 프로그램은 한 번 빌드해 임시 디렉터리에서 실행한다. 인증 없이 호출할 수 있는 경로는 설정의 `anonymous_operations`뿐이다(`API016`).

## Go 테스트의 잠금, 요구사항 추적, 품질 등급표 (ADR-0023)

- **잠금 단위.** 최상위 `TestXxx`, `FuzzXxx`, `ExampleXxx` 함수 하나다. 키는 `go:<패키지 디렉터리>::<함수 이름>`(예: `go:backend/internal/model::TestStandardCounts`)이라 TypeScript 키와 겹치지 않는다. `harness/config/test-lock.json`에 다른 테스트와 같은 형식으로 기록된다.
- **지문.** 함수 본문과, 본문이 이름으로 연쇄 참조하는 같은 패키지의 테스트 전용 도우미, 테이블, 상수, 골든 값, `testdata/` 아래 파일과 `go:embed`한 파일의 내용, 붙어 있는 `[REQ-NNN]` 태그다. 서식, 주석, 후행 쉼표는 지문에 포함되지 않는다. 테스트를 추가하는 것은 잠금을 깨지 않는다(`pnpm lock-tests`가 새 항목으로 기록한다). 삭제하거나 이름을 변경하거나 수정하면 `TLOCK001`, `TLOCK002`이고, 결정 기록의 `amends_tests`에 그 키가 있어야 해제된다.
- **약화.** TypeScript의 규칙을 Go로 옮긴 것이다: 실패를 호출하는 함수(`t.Fatal`, `t.Error`와 그 계열, 그리고 그것을 호출하는 도우미)의 수가 줄거나, 건너뛰기(`t.Skip` 계열)가 늘거나, 본문이 비면 지문이 달라지므로 `TLOCK001`이 이유를 밝힌다. 새 테스트에 실패를 호출하는 경로가 없으면 `GOTEST003`이다.
- **요구사항 추적.** `Test` 함수 바로 위 주석 행 `// [REQ-001] [REQ-002]`과 `t.Run`에 전달한 하위 테스트 이름의 `[REQ-NNN]`이 테스트와 요구사항을 연결한다(`TRACE001`, `TRACE002`, `TRACE006`이 TypeScript와 같이 확인한다). 어디에도 연결하지 못하는 태그(함수 본문 안의 주석, 한 괄호에 여러 id)는 `TRACE007`이다.
- **품질 등급표.** Go 단위는 `go:<단위>` 행이다. 같은 기준표로 등급이 산출되고 Go 측 입력은 `go test`의 문장 커버리지(`pnpm quality record`가 기록 파일에 기록한다), 크기와 복잡도, 검사 무시 수, 열린 부채다. 측정하지 못한 입력은 "알 수 없음"이다.
- **잠금에 포함하는 지원 코드(ADR-0028).** 테스트가 의존하는 비테스트 코드는 `go.locked_support`(패키지 디렉터리 또는 파일)에 적는다. 그 디렉터리의 비테스트 파일은 `package.Name`으로 참조하는 테스트(그 디렉터리의 테스트는 이름만으로도)의 지문에 포함된다. 지금은 적합성 테스트의 케이스 표(`backend/internal/graphcheck`)와 테스트가 사용하는 `graph/fixture.go`, `graph/scratch.go`이고, 케이스를 삭제하거나 단언을 약화하거나 추가하면 그것을 실행하는 테스트의 `TLOCK001`이 발생한다. 케이스의 이름과 순서는 `TestSuiteCaseNamesAreFixed`가 별도로 고정한다. 한계는 다음 항목과 같다.
- **한계.** 함수 이름으로 참조하는 범위를 넓게 잡아 지문은 관대하지 않고 오히려 엄격하다: 테스트가 사용하는 도우미의 본문을 수정해도 그 도우미를 쓰는 모든 테스트에서 걸린다. 다른 패키지의 도우미는 `locked_support`에 적은 것만 확인한다. 컴파일러가 아는 타입 해석은 하지 않는다. `testdata/`가 아닌 곳의 파일은 `go:embed`로 참조될 때만 확인한다.

## 임시 디렉터리 (ADR-0036)

임시 디렉터리는 만든 쪽이 지운다. 테스트 지원 코드(`harness/src/test-support`)를 import하는 테스트 파일은 `scratch-hook.ts`가 `TMPDIR`을 `<tmp>/praxium-scratch/run-<pid>-<임의 문자>/`로 바꾸고 파일이 끝날 때(실패와 시간 초과 포함) 지운다. 검사 코드는 `harness/src/scratch.ts`의 `openScratch`로 디렉터리를 얻고 `finally`에서 지운다. 강제 종료로 남은 것은 다음 실행이 시작할 때 지운다: 마지막 수정이 하루를 넘고 소유 프로세스가 없는 `run-*`만이고, 살아 있는 실행의 것과 다른 접두사의 것은 건드리지 않는다. 새 테스트 파일이 `mkdtempSync`나 `tmpdir()`을 쓰면 지원 코드를 import해야 한다(`scratch.test.ts`가 확인한다).

## 명령

- `pnpm check` — 전부 실행. 끝나기 전에 반드시 통과.
- `pnpm check:pr` — 기준 브랜치(origin/main)와 비교 포함 (CI와 동일).
- `pnpm ratchet` — 커버리지를 높였을 때 하한을 올리고, 해결한 부채 기준선 항목을 삭제하고, 줄어든 검사 무시 수의 기준선을 낮춘다.
- `pnpm tsx harness/src/run-all.ts --timing` — 검사마다 소요된 시간을 보여 준다.
- `pnpm quality write` — 품질 등급표(`docs/generated/quality.md`)를 다시 생성한다. 손으로 수정하지 않는다.
- `pnpm tsx harness/src/checks/docs.ts generate` — [결정 목록](../generated/decisions.md)과 [색인](../index.md)의 문서 목록을 한 번에 다시 생성한다. 변경은 이 파일들을 커밋하지 않는다: 오래되면 알림만 나오고, 손으로 수정하면 `DOC099`로 실패한다.
- `pnpm tsx harness/src/checks/docs.ts reverify <문서> --note "…"` — 문서가 오래된 대상 모두에 `docs/verified/` 기록을 쓴다. `--target <경로>`는 한 대상만 쓴다 (`DOC010`, `DOC011`). 대상이 이미 가진 기록과 같은 메모는 거절하고, 한 번에 10개를 넘는 대상은 `--target`으로 나눠 각각 자기 메모를 적게 한다 (ADR-0028).
- `pnpm tsx harness/src/checks/docs.ts seal-adr <ADR>` — 결정 기록을 확정한다. 확정한 뒤에는 본문을 수정할 수 없다 (`DOC070`).
- `pnpm tsx harness/src/checks/docs.ts link-amends <ADR>` — 새 ADR의 `amends`가 가리키는 ADR에 `amended_by`를 남긴다 (`DOC091`).
- `pnpm tsx harness/src/checks/docs.ts audit-queue` — 본문이 바뀌지 않은 채 다시 확인만 된 문서를 감사받은 지 오래된 순으로 보여 준다. 읽은 뒤 `docs.ts audit <문서> --note "한 줄"`.
- `pnpm tsx harness/src/checks/licenses.ts write` — 제3자 고지 파일(`THIRD_PARTY_NOTICES.md`)을 다시 생성한다. 손으로 수정하지 않는다.
- `pnpm lock-tests` — 새 테스트(Go 테스트 함수 포함)를 잠금 파일에 추가한다. 기존 항목은 변경하지 않는다. 잠긴 Go 테스트를 수정해야 하면 결정 기록의 `amends_tests`에 `go:<디렉터리>::<함수>`를 적고 확정한 뒤 `pnpm lock-tests --amend <ADR 파일>` ([ADR 절차](adr-process.md)).

## CI

`.github/workflows/ci.yml`이 병합 관문이다. 필수 검사는 `gate` 하나다. main에 병합한 뒤(push)에도 같은 전체 경로가 실행돼야 하고, `test` 잡은 측정한 커버리지를 기록 파일에 기록해(`quality.ts record`가 생성하고, 커밋하지 않는다) 품질 등급표의 커버리지 항목이 "알 수 없음"으로 남지 않게 한다.

- 일반 변경: `lint-types`(Biome와 타입), `test`(테스트와 커버리지를 한 번에 실행하고, 그 커버리지로 구조 검사 전체를 실행한다).
- `go-sum`: `backend/go.sum`의 줄을 공식 체크섬 데이터베이스에 다시 묻는다(`scripts/verify-go-sum.sh`, ADR-0032). 외부 모듈을 처음 들일 때 데이터베이스를 거치지 않았기 때문이다.
- 문서(`*.md`)만 바뀐 풀 리퀘스트: `docs`(구조 검사만).
- `.github/workflows/entropy.yml`은 아무도 푸시하지 않아도 주 1회 구조 검사를 실행한다. 문서는 시간이 지나면 오래된 것이 되고 부채는 기한이 도래한다.

## PostgreSQL이 필요한 Go 테스트 (ADR-0032)

`adapter.pgstore`의 테스트와 `api serve -store postgres`의 기동 테스트는 실제 PostgreSQL 16에서 돈다. 서버의 주소는 환경 변수 `PRAXIUM_TEST_PG_DSN`이 준다. **변수가 없으면 건너뛰지 않고 실패한다**(조용히 통과하는 경로를 두지 않는다). 테스트마다 새 데이터베이스를 만들고 끝나면 지운다.

- 로컬: `scripts/with-postgres.sh <명령>`이 일회용 서버를 소켓에만 띄우고 변수를 정해 명령을 실행한 뒤 서버와 데이터를 지운다. 예: `scripts/with-postgres.sh pnpm check`, `scripts/with-postgres.sh bash -c 'cd backend && go test ./...'`. 서버 프로그램(`initdb`, `pg_ctl`)이 있는 PostgreSQL 16이 필요하고, root이면 비특권 사용자로 띄운다. 변수가 이미 있으면(자기 서버, CI) 명령만 실행한다.
- CI: `test` 잡의 서비스 컨테이너(`postgres:16`)가 변수를 채운다.
- 기동 코드의 `os/exec`가 막혀 있어(`restricted_stdlib`) Go 테스트가 서버를 직접 띄우지 못하므로 서버는 테스트 바깥에서 준비한다.

## 리포지토리 호스트에서 활성화해야 하는 설정 (리포지토리 밖)

1. 기본 브랜치 보호: `gate` 통과 필수, 직접 push 금지.
2. "코드 소유자 승인 필수"를 쓸 때는 `.github/CODEOWNERS`의 소유자를 실제 사용자나 팀으로 둔다.

## 테라리움 하네스와의 동기화

하네스는 테라리움의 복사본이라 두 리포지토리에서 각각 수정된다(TD-0001). 테라리움에서 하네스가 수정되면 그 변경을 확인해 여기에 반영하고, 여기서 수정한 것은 테라리움에도 알린다. 가져오지 않은 것(UX 측정, 화면 도우미 검사)은 ADR-0002에 적혀 있다.
