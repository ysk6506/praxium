---
title: ADR-0019 하네스가 Go 코드를 읽는다 — Go 백엔드의 위치와 검사
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH020
  - GOARCH052
  - GODEP010
  - GODEBT022
  - GOIGN002
  - GOTEST002
  - GOCOV001
  - GOTOOL001
  - harness/src/checks/go-arch.test.ts
  - harness/src/checks/go-deps.test.ts
updates_docs:
  - AGENTS.md
  - ARCHITECTURE.md
  - docs/design-docs/harness-guide.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - harness/**
  - .github/**
  - docs/product-specs/requirements.md
amends_tests: []
decision_hash: 7ef852746cc50c17
amended_by:
  - adr: "0023"
    what: Go 테스트 잠금과 추적과 등급표
  - adr: "0024"
    what: 코어 유닛 `core.kernel`이 운영체제의 `os`를 import한다(`entropy.go` 한 파일에서 환경 변수를 읽는 입구)
  - adr: "0028"
    what: 구조 계약의 `go:` 절에 두는 필드와 import 규칙
---
# ADR-0019 하네스가 Go 코드를 읽는다 — Go 백엔드의 위치와 검사

## 맥락
백엔드는 Go로 만들기로 했다(ADR-0006). 그때 두 가지를 미뤘다. 하나는 Go 코드가 어디에 어떤 구조로 배치되는지이고, 다른 하나는 TypeScript 코드가 받는 규율(유닛 의존 방향, 시계와 난수 주입, 외부 모듈 신고, 크기 한도, 검사 무시 금지, 테스트와 커버리지)을 Go 코드도 받게 하는 일이다(부채 TD-0005). 첫 Go 제품 코드가 들어가기 전에 이 검사가 있어야 한다고 적었다. 검사가 없는 동안 쌓인 코드는 나중에 검사를 켤 때 한꺼번에 실패한다(ADR-0002).

이 변경이 첫 Go 코드(공통 기반 코드와 시작점의 뼈대)와 그것을 읽는 검사를 함께 도입한다.

## 결정
1. **Go 코드는 `backend/` 한 모듈에 둔다.** 모듈 경로는 `github.com/ysk6506/praxium/backend`다. 유닛은 `backend/internal/<유닛>` 패키지 하나이고, 이름은 ARCHITECTURE.md의 유닛 이름을 따른다(`core.kernel`은 `internal/kernel`, `core.contract`는 `internal/contract`). 유닛 안에서 하위 패키지는 자유롭게 나눌 수 있지만, 다른 유닛은 유닛의 루트 패키지로만 접근한다. 시작점은 `backend/cmd/praxium` 하나이고, 이것이 조립의 유일한 위치다. 역할 4종(`api`, `worker`, `query`, `collector`) 중 하나를 인자로 골라 기동한다. 이번 변경의 시작점은 역할을 해석하고 커널을 조립하고 구조화 로그 한 줄(`log/slog`)을 남긴 뒤 종료한다. 서버는 없다.
2. **유닛과 허용된 import 방향은 구조 계약 `harness/config/architecture.yaml`의 `go:` 절에 적는다.** 같은 파일의 TypeScript 유닛(`units:`)와 네임스페이스가 다르다(두 `core.kernel`은 다른 유닛다). `go:` 절에는 모듈 경로, 유닛(`path`, `may_import`), 조립 유닛(`composition_root`), 외부 모듈(`third_party`: 모듈 → 사용할 수 있는 유닛), 일부 표준 패키지를 사용할 수 있는 유닛(`restricted_stdlib`), 시계·난수·환경 변수를 읽을 수 있는 파일(`nondeterminism_allowed`)을 둔다. 새 유닛은 폴더가 생기는 변경에서 등록한다. 현재 유닛은 `core.kernel`(아무것도 import하지 않음), `core.contract`(`core.kernel`만), `cmd.praxium`(`core.*`)이다.
3. **하네스는 TypeScript 도구로 남고 Go를 읽는다.** 하네스를 Go로 바꾸지 않는다. Go 검사는 기존 검사 이름 안에서 실행된다. `common.test.ts`가 검사 이름 목록을 잠그고 있어서 새 이름을 추가하려면 잠긴 테스트를 바꿔야 하기 때문이다(라이선스 검사도 같은 방식을 택했다).

   | 검사 이름 | 코드 | 검증하는 내용 |
   |---|---|---|
   | `architecture` | `GOARCH001`, `GOARCH003`, `GOARCH004` | 모든 Go 파일은 유닛에 속하고, 유닛 폴더가 있고, `may_import`가 존재하는 유닛을 가리킨다 |
   | | `GOARCH010`, `GOARCH011`, `GOARCH014`, `GOARCH030` | 의존 방향, 공개 인터페이스(다른 유닛의 하위 패키지로 들어가지 않는다), 없는 패키지, 유닛 사이의 순환 |
   | | `GOARCH013`, `GOARCH020`, `GOARCH021` | 제품 코드가 `testing`을 import하지 않는다, 외부 모듈은 신고한 유닛에서만, 코어는 운영체제와 네트워크를 모른다(`os`, `net/http` 등은 지정한 유닛만) |
   | | `GOARCH040`, `GOARCH041` | 유닛은 자기 폴더에 Go 파일이 있고 패키지 설명(`// Package x ...`)이 있다 |
   | | `GOARCH050`, `GOARCH051`, `GOARCH053` | `unsafe`·cgo·`plugin`, 시작점 밖의 `os.Exit`·`log.Fatal*`, 점 import, 시작점 밖의 빈 import, `func init` |
   | | `GOARCH052` | 시계·난수·환경 변수를 지정된 파일 밖에서 읽지 않는다: `time.Now`, `time.Since`, `time.Until`, `os.Getenv`, `os.LookupEnv`, `os.Environ`, `math/rand`, `math/rand/v2`, `crypto/rand`. 테스트 파일도 같다(REQ-001의 Go 측) |
   | | `GODEP001`, `GODEP010`~`GODEP014`, `GODEP020` | `go.mod`의 모든 `require`(간접 포함)가 ARCHITECTURE.md의 모듈 표에 한 행(모듈, 라이선스, 이유)으로 있고, 라이선스가 허용 목록 안이고, `go.third_party`와 일치하고, `replace`와 `exclude`가 없다(REQ-239) |
   | | `GOTOOL000`~`GOTOOL002` | Go가 설치돼 있고, `gofmt -l` 출력이 비어 있고, `go vet ./...`가 통과한다 |
   | `debt` | `GODEBT001`, `GODEBT002`, `GODEBT013` | 추적 표시 주석은 대장의 열린 항목을 가리킨다 |
   | | `GODEBT020`~`GODEBT023`, `GODEBT032` | 파일 500줄, 함수 80줄, 복잡도 12, 매개변수 6개. 한도는 TypeScript와 같은 `debt.yaml`을 읽는다. 테스트 파일은 측정하지 않는다. 기준선이 없어 한도를 넘으면 `// debt-ok: TD-NNNN`이 있어야 한다 |
   | `ignores` | `GOIGN001`~`GOIGN004` | `//nolint`, `//lint:ignore`, `//go:build ignore`, `//nosec`, 건너뛴 테스트(`t.Skip`, `t.Skipf`, `t.SkipNow`, `testing.Short()` 분리)는 이유와 열린 `TD-NNNN` 또는 확정된 `ADR-NNNN`이 있어야 한다. Go 코드는 하나도 없이 시작하고 기준선이 없다 |
   | `test-integrity` | `GOTEST002` | 함수가 있는 유닛은 `Test` 함수가 있는 테스트 파일을 자기 폴더에 갖는다 |
   | `coverage` | `GOTEST001`, `GOCOV001`, `GOCOV002` | `go test`가 통과하고, 유닛마다 문장 커버리지가 하한 이상이다. 하한은 `coverage-floor.json`의 `go:<유닛>`이고 최소 90, `pnpm ratchet`이 올린다. 낮추면 `COV003`이 잡는다 |

4. **import는 `go list`가 아니라 소스에서 읽는다.** 작은 어휘 분석기(`go-lex.ts`)가 문자열, 원시 문자열, 룬, 주석을 따로 분리한 뒤 import 선언, 패키지 절, 함수(크기, 매개변수, 복잡도), `패키지.이름` 사용을 읽는다. 테스트 파일, 빌드 태그가 붙은 파일, 별칭, 파일별 줄 번호를 한 번에 보기 쉽고 하위 프로세스 하나를 덜 호출하기 때문이다. 없는 패키지나 타입 오류처럼 컴파일러만 아는 것은 `go vet`이 본다. 표준 라이브러리인지는 첫 경로 요소에 점이 있는지로 구분한다(Go의 규칙). 복잡도는 `if`, `for`, `case`, `&&`, `||`를 센 근삿값이다.
5. **Go가 필요하다.** 하네스는 `gofmt`, `go vet`, `go test`를 호출한다. 다른 Go를 내려받지 않고(`GOTOOLCHAIN=local`), 작업 공간 파일과 모듈 파일을 수정하지 않는다. CI의 설정 액션(`.github/actions/setup`)이 `backend/go.mod`의 버전으로 Go를 설치하고, 구조 검사를 실행하는 모든 작업이 이것을 쓴다. `pnpm check`는 그대로이고 Go 검사를 포함한다.
6. **공통 기반 코드(`internal/kernel`)를 함께 도입한다.** 표준 라이브러리만 쓴다(외부 모듈 없음).
   - 타입 있는 ID(`ID[K]`, 구분용 타입 `K`로 서로 섞이지 않는다. TypeScript와 같은 문자 규칙으로 검증한다)와 ID 발급 포트(`IDGenerator`)의 구현 2가지(`SequentialIDs`, 난수를 받는 `RandomIDs`).
   - 안정된 코드를 가진 오류(`Error`, `Code`)와 코드 목록의 시드(`internal`, `invalid_value`, `not_found`, `conflict`, `unsupported`, `read_only_violation`). 코드는 이름을 바꾸거나 재사용하지 않는다. 테스트가 목록을 잠가 두므로 코드를 추가하는 일은 의도적으로 해야 한다. HTTP 상태로의 매핑은 계약 유닛의 몫이라 커널에 두지 않는다.
   - `Clock`과 `Rng`의 포트와 구현(`ManualClock`, 그리고 splitmix64인 `SeededRng`. `Fork(라벨)`이 라벨마다 서로 독립적인 난수 스트림을 만든다). 시계와 난수를 직접 읽는 곳은 `entropy.go` 한 파일(`SystemClock`, `SystemSeed`, `NewToken`)이다.
   - 안정된 내용 해시: 정규 바이너리 인코딩(버전 1) 위의 SHA-256(ADR-0006 8번, TD-0004의 Go 측). 인코딩은 `hash.go`에 적혀 있다. 널, 거짓/참, 정수(8바이트), 문자열(길이와 UTF-8), 목록, 맵(키의 바이트 순서)만 있고, 부동소수점, 구조체, 올바르지 않은 UTF-8, int64를 넘는 부호 없는 정수는 오류다. 정해진 값(골든)은 이 구현이 아닌 다른 구현으로 계산해 테스트에 넣었다. 인코딩을 바꾸는 일은 수정이 아니라 새 버전이다.
7. **빈 `internal/contract` 유닛을 먼저 등록한다.** 외부로 내보내는 형식은 계약이 먼저(ADR-0016)이기 때문에 위치를 먼저 잡고 패키지 설명만 둔다.
8. **부채 TD-0005를 닫는다.** 해결: 이 결정이 만든 검사. 남은 것은 새 부채 TD-0008로 적는다.
9. **요구사항.** REQ-239는 `GODEP*`가 검사한다(예정 표시를 뗀다). REQ-001, REQ-021, REQ-022, REQ-023의 본문에 Go 코드도 같은 규율을 받는다는 내용을 추가한다.

## 아직 검사하지 않는 것, 이유와 다음 단계
- **Go 테스트의 잠금**(테스트를 삭제하거나 약화하거나 기대값을 바꾸는 것 막기. TypeScript의 `TLOCK*`에 해당): 테스트 함수의 지문을 정하고 기대값 변경을 가려내는 일이 따로 큰 일이다. 기계가 막지 못하는 동안 Go 테스트를 수정하는 변경은 리뷰가 지킨다. 새 유닛의 테스트가 이제 막 생겨 잠글 것이 적다는 점이 미루는 이유이다. TD-0008.
- **품질 등급표와 요구사항 추적이 Go를 읽는 일**: 등급표는 유닛 이름이 TypeScript 유닛과 겹쳐 따로 정해야 하고, 추적은 Go 테스트 제목에서 `[REQ-NNN]`을 읽는 규칙이 필요하다. 이번에는 Go 테스트가 요구사항 번호를 적어도 기계가 집계하지 않는다. TD-0008.
- **모듈 표의 라이선스가 사실인지**: 표에 적힌 라이선스를 허용 목록과 비교할 뿐, 모듈의 실제 라이선스 파일을 읽지 않는다. 지금은 모듈이 없다. 첫 모듈을 도입하는 변경이 `go.sum`과 모듈 캐시를 읽는 검사를 함께 만든다(TypeScript의 `LIC*`가 설치된 패키지를 읽는 것처럼).
- **타입 정보가 필요한 규칙**: 포트가 사용하는 쪽 코어 유닛에 있는지, 공개 인터페이스에 내보낸 이름이 적절한지, 오류를 버리지 않았는지(`errcheck` 같은 것)는 보지 않는다. `go vet`의 기본 검사가 전부다.
- **동시성과 성능**: `-race`, 퍼징, 벤치마크 성능 저하, `govulncheck`는 실행하지 않는다. 해당하는 코드가 생기는 변경에서 추가한다.
- **시계 읽기의 간접 경로**: `time.Now`를 변수에 담아 호출하는 것은 잡지만(`now := time.Now`), 리플렉션이나 다른 패키지를 거친 간접 호출은 보지 못한다. 그것은 그 패키지의 import 규칙이 막는다.
- **하위 패키지끼리의 규칙**: 한 유닛 안에서 하위 패키지가 서로 접근하는 방식은 검사하지 않는다. 유닛 안은 자유롭다.

## 이유
- 검사가 코드보다 먼저 있으면 처음부터 같은 규율로 성장한다. 나중에 켜면 한꺼번에 실패한다.
- 유닛이 패키지 하나이고 유닛 사이의 import 방향이 `may_import`로 적혀 있으면, 에이전트가 헤매지 않고 검사 메시지를 믿고 수정할 수 있다.
- 시계, 난수, 환경 변수를 한 파일에 모으면 결정성(REQ-001)을 코드 읽기 없이 기계가 지킬 수 있다.
- 한도와 대장을 TypeScript와 같은 것으로 쓰면 규칙이 둘로 갈라지지 않는다.
- 어휘 분석기는 npm 의존성이 필요 없고, 사용하는 부분 집합이 좁아 틀리기 어렵다. 틀리는 부분은 테스트가 잡는다.

## 버린 대안
- **하네스를 Go로 다시 쓰기**: 하네스의 대부분(문서, 부채, 테스트 잠금, 요구사항 추적)은 언어와 무관하다. 다시 쓰는 비용이 크고 얻는 것이 없다(ADR-0006 3번).
- **`go list -json`으로 import 읽기**: 컴파일러가 해석한 결과라 정확하지만 빌드 태그에 따라 파일이 빠지고, 테스트 파일과 파일별 줄 번호를 따로 모아야 하고, 소스가 깨져 있으면 아무것도 읽지 못한다.
- **golangci-lint와 `depguard` 같은 외부 도구에 맡기기**: 외부 도구와 그 설정이 한 계층 더 생기고, 이 리포지토리의 대장과 ADR에 묶이는 규칙(`TD-NNNN`, 모듈 표)을 적용할 수 없다.
- **Go 유닛을 `units:`에 섞어 적기**: TypeScript 검사들이 유닛 이름과 폴더 규칙을 가정하고 있어 충돌한다. 네임스페이스를 나눈 쪽이 단순하다.
- **새 검사 이름(`go`) 추가하기**: 잠긴 테스트를 수정해야 한다. 이미 있는 검사 이름 안에서 코드 접두어로 구분하는 방식이 같은 효과를 낸다.
- **Go 테스트 잠금까지 이번에 만들기**: 가치에 비해 크고, 유닛이 둘뿐인 지금은 리뷰로 버틸 수 있다. 부채로 적고 기한을 둔다.

## 결과
Go 모듈, `core.kernel`, 빈 `core.contract`, 시작점 `cmd.praxium`이 들어갔고, 하네스가 이것을 읽는다. 검사마다 위반하는 작은 Go 모듈과 통과하는 작은 Go 모듈을 가지고 "정말 잡는가"를 확인하는 테스트가 있다. 구조 검사와 테스트, `go test`, `go vet`이 통과한다. 부채 TD-0005를 닫고 TD-0008을 열었다.
