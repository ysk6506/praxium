---
title: AGENTS.md — 에이전트 진입점
kind: guide
status: active
covers:
  - package.json
  - harness/config/architecture.yaml
  - harness/config/protected-paths.txt
---
# Praxium — 코딩 에이전트 안내서

IT 인프라를 믿을 수 있는 지식 그래프(KG)로 만드는 오픈소스 플랫폼. 그 위에서 AI 에이전트에게 운영 업무를 위임하고 결과를 측정하는 것이 목표다. 백엔드는 Go로 구현해 `backend/`에 두고(ADR-0006, ADR-0019), 화면은 다른 리포지토리에서 REST API 계약으로만 통신한다(ADR-0016). 지금 있는 TypeScript는 구조 검사(하네스)와 프런트엔드 리포지토리로 옮길 공통 기반 코드다. 이 파일은 **지도**다.

## 시작하기 (3분)

0. 제품 설계 개요: [product-design.md](docs/design-docs/product-design.md) — 제품의 가치, 원칙, 쟁점별 답, 순서, 설계 문서 지도. 설계와 구현 전에 먼저 읽는다.
1. 요구사항: [requirements.md](docs/product-specs/requirements.md) — 목표와 `REQ-NNN`.
2. 구조: [ARCHITECTURE.md](ARCHITECTURE.md) — 유닛과 의존 방향.
3. 문서 색인: [docs/index.md](docs/index.md)
4. 현재 진행 중인 작업: [진행 중 계획](docs/exec-plans/active/plan.md)
5. 테라리움에서 인계받은 것: [from-terrarium.md](docs/references/from-terrarium.md) — 출발점이 된 자료였고, 쟁점의 답은 결정 기록(ADR-0005~0018)에 있다. 어느 쟁점이 어디서 답을 얻었는지는 제품 설계 개요의 지도를 본다.

```
pnpm install
pnpm check        # lint + 타입 + 테스트 + 모든 구조 검사(Go의 gofmt, vet, 테스트 포함). 이것이 통과해야 완료다.
cd backend && go test ./... && go vet ./...   # Go만 빠르게 확인할 때. Go 1.27.4 이상이 있어야 한다.
go run ./backend/cmd/praxium worldgen -scale 100000   # 합성 세계(생성기). 인자는 docs/design-docs/worldgen.md
```

세계를 합성해 개발 모드 API에 적재하고 명령줄로 읽는 가장 짧은 절차(개발과 테스트의 편의이지 수집 파이프라인이 아니다. 토큰은 임의로 만든 값이다, [ADR-0027](docs/design-docs/adr/0027-reading-the-kg-through-the-api-and-the-command-line.md)):

```
cd backend && go build -o /tmp/praxium ./cmd/praxium
/tmp/praxium worldgen -scale 500 -seed 7 -out /tmp/world
printf 'dev-token-not-a-secret' > /tmp/dev.tok
/tmp/praxium api serve -mode development -dev-token-file /tmp/dev.tok -dev-truth /tmp/world/truth.jsonl &
/tmp/praxium --endpoint http://127.0.0.1:8080 --token dev-token-not-a-secret kg list
/tmp/praxium --endpoint http://127.0.0.1:8080 --token dev-token-not-a-secret query find --type PhysicalServer --limit 3
```

파일 소스 하나를 수집 파이프라인으로 KG에 넣는 가장 짧은 경로(소스 정의와 매핑 정의는 JSON 데이터이고 샘플 파일은 합성한 값이다, [ADR-0033](docs/design-docs/adr/0033-ingestion-vertical-slice-from-a-file-source.md)). `-times 2`는 같은 파일로 한 번 더 실행해 두 번째 실행이 아무것도 바꾸지 않음을 보인다:

```
cd backend && go build -o /tmp/praxium ./cmd/praxium
D=internal/ingestcheck/testdata
/tmp/praxium ingest run -source $D/source.json -mapping $D/mapping.json -times 2 \
  -input ds_racks=$D/sample/racks.csv -input ds_servers=$D/sample/servers.csv \
  -input ds_hosts=$D/sample/hosts.jsonl -input ds_vms=$D/sample/vms.jsonl -input ds_vm_power=$D/sample/vm-power.csv
```

CI가 병합 관문이다: main에는 풀 리퀘스트로만 반영되고, 필수 검사 `gate`가 통과할 때만 병합한다. 문서(`*.md`)만 변경한 풀 리퀘스트는 경량 경로를 거쳐 구조 검사만 실행된다([ADR-0002](docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md)).

실패 메시지는 `problem`과 `fix`를 알려 준다. 메시지를 따라 그대로 수정한다.

## 절대 규칙 (기계가 강제함)

| 하지 말 것 | 강제 장치 |
|---|---|
| 테스트가 실패하는데 테스트를 수정하기 | 테스트는 잠겨 있다 (`TLOCK*`, Go 테스트 함수도 `go:<디렉터리>::<함수>` 키로). 코드를 수정한다. 요구사항이 변경된 것이 확실할 때만 결정 기록의 `amends_tests` 절차 |
| skip/only/삭제/약화, 커버리지 제외. Go는 상수로 결정되는 단언, 에러를 무시하는 테스트, 다른 테스트를 숨기는 `TestMain` | `TEST*`, `TCFG*`, `COV*`, Go는 `GOTEST004`~`GOTEST006` |
| 유닛 의존 방향 위반, 다른 유닛 내부 import. 테스트 파일에만 허용된 유닛(`test_may_import`)을 제품 코드에서 import | `ARCH010`, `ARCH011`, Go는 `GOARCH010`, `GOARCH011` |
| Go import 경로를 이스케이프로 작성하기, 테스트 파일에 빌드 제약(`//go:build`, `_linux_test.go`) 지정하기, 실행 파일·테스트 바이너리 커밋 | `GOARCH054`, `GOARCH055`, `GOARCH056` |
| 선언되지 않은 패키지·Go 모듈 추가 | `ARCH020`, Go는 `GOARCH020`, `GODEP010` — ADR + 계약 등록 + ARCHITECTURE.md의 모듈 표 |
| `harness/`, 설정 파일, ADR, 이 파일 수정 | 보호 경로 (`PROT001`) — 같은 변경에 확정된 ADR 필요 |
| 검사 무시 주석(`biome-ignore`, Go의 `//nolint` 등)·스킵된 테스트·CI `continue-on-error`를 이유와 근거 없이 두기, 그 수를 늘리기 | `IGN001`~`IGN007`, Go는 `GOIGN001`~`GOIGN004` — 같은 줄이나 바로 위 줄에 이유와 `TD-NNNN`(열린 부채) 또는 `ADR-NNNN`을 적는다. 원인을 해결하는 것이 먼저다 |
| 품질 등급표·결정 목록·색인의 문서 목록·고지 파일을 수동으로 수정하기 | 하네스가 생성한다. 오래되면 알림 한 줄만 나오고, 수동으로 수정하면 `DOC099`로 실패한다 |
| 허용 목록 밖의 라이선스를 가진 의존성·자료를 함께 배포하기 | `LIC001`, `LIC002` — 의존성을 도입하기 전에 라이선스를 확인한다 |
| 추적 없는 `TODO` | `TODO(TD-NNNN)` + [부채 대장](docs/exec-plans/tech-debt.yaml) (`DEBT001`, Go는 `GODEBT001`) |
| 내부에서 알게 된 사실(어느 조직이 무엇을 개발·운영하는지, 내부 시스템의 이름·구조·규모·제조사, 조직과 절차), 실제 데이터, 시크릿을 코드·문서·PR·이슈·커밋에 적기 | 리포지토리는 공개돼 있다. 기계가 구분하지 못하므로 작성하는 쪽이 지킨다. 요구사항의 형태로 바꿔 적고, 샘플은 합성한다 ([ADR-0003](docs/design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md)) |
| `Date.now()`, `Math.random()`, Go의 `time.Now`, `os.Getenv`, `math/rand` 직접 사용 | `kernel`의 `Clock`/`Rng`를 주입받는다 (`ARCH052`, `GOARCH052`) |
| 코드만 변경하고 문서 방치 | 문서가 `covers`한 코드나 `follows`한 문서가 변경되면 실패 (`DOC010`). 문서가 가리키는 경로·명령·코드·숫자가 없으면 실패 (`DOC094`, `DOC095`). 결정이 변경하는 문서를 같은 변경에서 수정하지 않으면 실패 (`DOC090`) |

## 설계할 때 (기계가 못 지키는 것)

- 현실 시스템에는 읽기만 한다. 실제 툴은 호출하지 않는다: 호출하는 것은 에이전트이고, 통제하는 것은 MCP 게이트웨이다(ADR-0001, ADR-0008).
- 모델은 프락시움이 소유한다(타입 이름은 영어). 위임 전의 검증은 제품 밖의 일이다(ADR-0005, ADR-0007).
- 용도에 맞는 언어와 스토리지를 쓰되, 단일 진실 공급원은 하나이고 의미의 구현은 하나다(ADR-0006). 계약(OpenAPI)이 먼저다(ADR-0016). 계약은 `backend/internal/contract/openapi/parts`에 조각(fragment)으로 두고 `go run ./cmd/praxium contract assemble`로 병합한다. 경로를 추가하거나 변경하면 조각, 병합한 것, 서버를 함께 수정한다(`API*`, ADR-0024).
- 모든 변경은 정의의 변경이다. 브랜치와 제안을 거치고, 리버트는 앞으로 나아가는 새 변경이다(ADR-0014). 프락시움도 에이전트가 운영할 수 있게 만든다(ADR-0017).
- 요구사항을 받으면 그것만 보고 기능을 덧붙이지 않는다. 제품 설계 개요의 원칙에 비춰 보고, 영향받는 기존 자산을 전부 찾고, 어긋나면 먼저 알린다.
- 같은 주제의 결정 기록을 하루에 2개를 넘게 쓰게 되면 멈추고 전체를 다시 본다.
- 계획은 날짜가 아니라 순서로 관리한다.
- 화면 코드는 이 리포지토리에 없다. 예외는 삭제할 수 있는 임시 콘솔 하나이고 공개된 `/v1`만 부른다(ADR-0016, TD-0007).
- 문서와 화면에는 한국의 IT 실무자가 쓰는 표준 용어를 쓰고 새 용어를 만들지 않는다. 사전은 [용어집](docs/design-docs/glossary.md)의 표 하나이고, 옛 표기는 `WORD003`이 알린다.

## 자주 하는 일

- **`DOC010`** → 문서를 코드와 대조해 수정하고 `pnpm tsx harness/src/checks/docs.ts reverify <문서> --note "확인한 내용"`. 확인 상태는 `docs/verified/`의 (문서, 대상) 기록에 있다.
- **새 테스트 추가** → 제목에 `[REQ-NNN]`을 넣고(Go는 `Test` 함수 바로 위 주석 `// [REQ-NNN] [REQ-MMM]`), 작성 후 `pnpm lock-tests` (새 항목만 추가됨).
- **새 유닛(디렉터리) 추가** → 같은 변경에서 `harness/config/architecture.yaml`에 등록하고(Go는 `go:` 절, 디렉터리는 `backend/internal/<유닛>`) ARCHITECTURE.md를 수정한다(보호 경로라 ADR이 필요하다). 테스트에서만 쓰는 유닛은 `may_import`가 아니라 `test_may_import`에 적는다.
- **새 결정** → [ADR 작성 절차](docs/design-docs/adr-process.md). `enforced_by`로 강제 장치에 연결하고, `updates_docs`에 변경하는 문서를 적어 같은 변경에서 수정한다. 앞선 결정의 일부를 변경하면 `amends`를 적고 `docs.ts link-amends <ADR>`를 돌린다.
- **부채를 남겨야 함** → 대장에 항목 등록 후 `TODO(TD-NNNN)`. 기한과 예산이 있다.
- **커버리지를 높였다** → `pnpm ratchet`으로 하한을 고정. 하한이 측정한 값보다 1점 넘게 낮으면 `COV006`, `GOCOV003`으로 실패한다.
- **정비 작업** → `pnpm test`로 커버리지를 측정한 뒤 `pnpm tsx harness/src/checks/quality.ts record`로 기록하고(CI도 같은 기록을 만든다), `pnpm quality write`로 [품질 등급표](docs/generated/quality.md)를, `pnpm tsx harness/src/checks/docs.ts generate`로 [결정 목록](docs/generated/decisions.md)과 색인의 문서 목록을 다시 생성한다. 일반 변경은 이 생성물을 커밋하지 않는다. 이어서 `docs.ts audit-queue`의 앞 3개를 독립적으로 검토하고 `docs.ts audit <문서> --note "한 줄"`.

## 철학

- 리포지토리에 없는 지식은 존재하지 않는다.
- 경계는 엄격히, 경계 안의 구현은 자유롭게.
- 제품 목표와 요구사항은 제품 책임자가 정하고, 설계는 그것을 기준으로 정해 ADR에 이유와 함께 남긴다.
