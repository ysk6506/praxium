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

현실의 IT 인프라를 데이터로 받아 운영 세계를 짓고, 그 위에서 에이전트가 맡는 E2E 프로세스를 정하고 맡기고 재는 사내 운영 관리 플랫폼. TypeScript 하나로 만든다. 이 파일은 **지도**다.

## 시작하기 (3분)

0. 제품 설계 한 장: [product-design.md](docs/design-docs/product-design.md) — 제품의 값, 원칙, 넘겨받은 숙제, 순서. 설계와 구현 전에 먼저 읽는다.
1. 요구사항: [requirements.md](docs/product-specs/requirements.md) — 목표와 `REQ-NNN`.
2. 구조: [ARCHITECTURE.md](ARCHITECTURE.md) — 단위와 의존 방향.
3. 문서 색인: [docs/index.md](docs/index.md)
4. 지금 하던 일: [진행 중 계획](docs/exec-plans/active/plan.md)
5. 테라리움에서 넘겨받은 것: [from-terrarium.md](docs/references/from-terrarium.md) — 그때의 요구사항, 설계 결정, 풀리지 않은 쟁점. 출발 자료이지 결정이 아니다. 제품 설계를 시작하기 전에 6절부터 읽는다(ADR-0004).

```
pnpm install
pnpm check        # lint + 타입 + 테스트 + 모든 구조 검사. 이게 통과해야 끝난 것.
```

CI가 합치기 관문이다: main에는 풀 리퀘스트로만 들어가고, 필수 검사 `gate`가 초록일 때만 합친다. 문서(`*.md`)만 바꾼 풀 리퀘스트는 가벼운 경로를 타서 구조 검사만 돈다([ADR-0002](docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md)).

실패 메시지는 `problem`과 `fix`를 알려준다. 메시지를 믿고 그대로 고친다.

## 절대 규칙 (기계가 강제함)

| 하지 말 것 | 강제 장치 |
|---|---|
| 테스트가 실패하는데 테스트를 고치기 | 테스트는 잠겨 있다 (`TLOCK*`). 코드를 고친다. 요구사항이 바뀐 게 확실할 때만 결정 기록의 `amends_tests` 절차 |
| skip/only/삭제/약화, 커버리지 제외 | `TEST*`, `TCFG*`, `COV*` |
| 단위 의존 방향 위반, 다른 단위 내부 import | `ARCH010`, `ARCH011` |
| 선언 안 된 패키지 추가 | `ARCH020` — ADR + 계약 등록 |
| `harness/`, 설정 파일, ADR, 이 파일 수정 | 보호 경로 (`PROT001`) — 같은 변경에 봉인된 ADR 필요 |
| 검사 끄기 주석(`biome-ignore` 등)·건너뛴 테스트·CI `continue-on-error`를 이유와 근거 없이 두기, 그 수를 늘리기 | `IGN001`~`IGN007` — 같은 줄이나 윗줄에 이유와 `TD-NNNN`(열린 부채) 또는 `ADR-NNNN`을 적는다. 원인을 고치는 쪽이 먼저다 |
| 품질 등급표·결정 목록·색인의 문서 목록·고지 파일을 손으로 고치기 | 하네스가 만든다. 낡으면 알림 한 줄만 나오고, 손으로 고치면 `DOC099`로 실패한다 |
| 허용 목록 밖의 라이선스를 가진 의존성·자료를 함께 내보내기 | `LIC001`, `LIC002` — 의존성을 들이기 전에 라이선스를 본다 |
| 추적 없는 `TODO` | `TODO(TD-NNNN)` + [부채 장부](docs/exec-plans/tech-debt.yaml) (`DEBT001`) |
| 내부에서 들은 사실(어느 조직이 무엇을 개발·운영하는지, 내부 시스템의 이름·구조·규모·제조사, 조직과 절차), 실제 데이터, 비밀 값을 코드·문서·PR·이슈·커밋에 적기 | 저장소는 공개돼 있다. 기계가 못 가리니 쓰는 쪽이 지킨다. 요구의 말로 바꿔 적고, 견본은 지어낸다 ([ADR-0003](docs/design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md)) |
| `Date.now()`, `Math.random()` 직접 사용 | `kernel`의 `Clock`/`Rng`를 주입받는다 (`ARCH052`) |
| 코드만 바꾸고 문서 방치 | 문서가 `covers`한 코드나 `follows`한 문서가 바뀌면 실패 (`DOC010`). 문서가 가리키는 길·명령·코드·숫자가 없으면 실패 (`DOC094`, `DOC095`). 결정이 바꾸는 문서를 같은 변경에서 안 고치면 실패 (`DOC090`) |

## 설계할 때 (기계가 못 지키는 것)

- 현실 시스템에는 읽기만 한다. 실제 툴은 부르지 않는다: 부르는 것은 에이전트이고, 막는 것은 MCP 게이트웨이다(ADR-0001).
- 요구사항을 받으면 그것만 보고 덧붙이지 않는다. 제품 설계 한 장의 원칙에 대 보고, 걸리는 기존 자산을 전부 찾고, 어긋나면 먼저 말한다.
- 같은 주제의 결정 기록을 하루에 둘 넘게 쓰게 되면 멈추고 전체를 다시 본다.
- 계획은 날짜가 아니라 순서로 관리한다.
- 화면의 말은 현업에서 쓰는 말을 쓰고 지어내지 않는다([용어집](docs/design-docs/glossary.md)).

## 자주 하는 일

- **`DOC010`** → 문서를 코드와 대조해 고치고 `pnpm tsx harness/src/checks/docs.ts reverify <문서> --note "확인한 것"`. 확인 상태는 `docs/verified/`의 (문서, 대상) 기록에 있다.
- **새 테스트 추가** → 제목에 `[REQ-NNN]`을 넣고, 작성 후 `pnpm lock-tests` (새 항목만 추가됨).
- **새 단위(폴더) 추가** → 같은 변경에서 `harness/config/architecture.yaml`에 등록하고 ARCHITECTURE.md를 고친다(보호 경로라 ADR이 필요하다).
- **새 결정** → [ADR 작성 절차](docs/design-docs/adr-process.md). `enforced_by`로 강제 장치에 연결하고, `updates_docs`에 바꾸는 문서를 적어 같은 변경에서 고친다. 앞선 결정의 일부를 바꾸면 `amends`를 적고 `docs.ts link-amends <ADR>`를 돌린다.
- **부채를 남겨야 함** → 장부에 항목 등록 후 `TODO(TD-NNNN)`. 기한과 예산이 있다.
- **커버리지를 올렸다** → `pnpm ratchet`으로 하한을 고정.
- **정리 작업** → `pnpm quality write`로 [품질 등급표](docs/generated/quality.md)를, `pnpm tsx harness/src/checks/docs.ts generate`로 [결정 목록](docs/generated/decisions.md)과 색인의 문서 목록을 새로 만든다. 일반 변경은 이 생성물을 커밋하지 않는다. 이어서 `docs.ts audit-queue`의 앞 셋을 독립된 눈으로 읽고 `docs.ts audit <문서> --note "한 줄"`.

## 철학

- 저장소에 없는 지식은 존재하지 않는다.
- 경계는 엄격히, 경계 안의 구현은 자유롭게.
- 제품 목표와 요구사항은 제품 책임자가 정하고, 설계는 그것을 기준으로 정해 ADR에 이유와 함께 남긴다.
