---
title: 품질 하네스 사용법
kind: guide
status: active
covers:
  - harness/src/run-all.ts
---
# 품질 하네스

`harness/`는 저장소가 오래 건강하도록 지키는 검사 모음이다. 각 검사는 자신의 메타 테스트(`harness/src/checks/*.test.ts`)로 "정말 위반을 잡는가"가 검증되어 있다. 테라리움의 하네스를 가져온 것이다(ADR-0002).

| 요구 | 검사 | 코드 |
|---|---|---|
| 문서 품질 (REQ-020) | `harness/src/checks/docs.ts` | `DOC*` |
| 아키텍처 강제 (REQ-021) | `harness/src/checks/architecture.ts` | `ARCH*` |
| 제3자 라이선스 (함께 나가는 npm 패키지와 자료는 허용 목록 안의 라이선스여야 한다) | `harness/src/checks/licenses.ts` | `LIC*` |
| 기술 부채 (REQ-022) | `harness/src/checks/debt.ts` | `DEBT*` |
| 검사 끄기 금지 (끄기 주석·건너뛴 시험·CI `continue-on-error`는 이유와 근거 번호가 있어야 하고 수는 늘 수 없다) | `harness/src/checks/ignores.ts` | `IGN*` |
| 테스트 변조 방지 (REQ-023) | `harness/src/checks/testguard.ts` | `TEST*` `TCFG*` `TLOCK*` |
| 커버리지 (REQ-023) | `harness/src/checks/coverage.ts` | `COV*` |
| 요구사항 추적 (`**예정(ADR-NNNN)**` 요구는 따로 나열) | `harness/src/checks/traceability.ts` | `TRACE*` |
| 화면의 말 (한 가지 뜻에 한 가지 말) | `harness/src/checks/ui-words.ts` | `WORD*` |
| 품질 등급표 (REQ-024, 단위별 등급 A~D, 하네스가 만드는 생성물) | `harness/src/checks/quality.ts` | `QUAL*` |
| 가드레일 보호 | `harness/src/checks/protected.ts` | `PROT*` |
| Go 코드(ADR-0019): 단위와 import, 공개 면, 시계·난수·환경, 모듈 표, 크기, 끄기, 시험, 형식 | `harness/src/checks/go-*.ts` | `GOARCH*` `GODEP*` `GODEBT*` `GOIGN*` `GOTEST*` `GOCOV*` `GOTOOL*` |

설정은 `harness/config/`에 있고 보호 경로다. 바꾸려면 같은 변경에 봉인된 결정 기록이 있어야 한다.

## Go 코드를 읽는 검사 (ADR-0019)

하네스는 TypeScript 도구로 남고 Go 코드를 읽는다. Go 검사는 이미 있는 검사 이름 안에서 돈다(검사 이름의 목록은 `common.test.ts`가 잠그고 있다): `architecture`에서 `GOARCH*`(단위, import, 공개 면, 시계·난수·환경, 금지 구문), `GODEP*`(`go.mod`와 ARCHITECTURE.md의 모듈 표), `GOTOOL*`(`gofmt`, `go vet`), `debt`에서 `GODEBT*`, `ignores`에서 `GOIGN*`, `test-integrity`에서 `GOTEST002`(시험 없는 단위), `coverage`에서 `GOTEST001`(`go test` 실패)과 `GOCOV*`(단위의 문장 커버리지 하한)다. 구조 계약은 `harness/config/architecture.yaml`의 `go:` 절이고, 한도와 장부는 TypeScript와 같은 `debt.yaml`과 장부를 쓴다.

- import는 `go list`가 아니라 소스에서 읽는다(`go-lex.ts`). 시험 파일, 빌드 태그가 붙은 파일, 별칭, 줄 번호를 한 번에 보려는 것이다. 컴파일러가 아는 것(타입 오류, 없는 패키지)은 `go vet`이 본다.
- Go가 설치돼 있어야 한다(`GOTOOL000`). 하네스는 `GOTOOLCHAIN=local`로 다른 Go를 내려받지 않고, 모듈 파일을 고치지 않는다.
- 커버리지 하한은 `harness/config/coverage-floor.json`에 `go:<단위>`로 있고, `pnpm ratchet`이 올린다.
- 아직 보지 않는 것은 ADR-0019에 적혀 있다(시험 잠금, 품질 등급표, 시험 제목의 요구사항 추적. 부채 TD-0008).

## 명령

- `pnpm check` — 전부 실행. 끝나기 전에 반드시 통과.
- `pnpm check:pr` — 기준 브랜치(origin/main)와 비교 포함 (CI와 동일).
- `pnpm ratchet` — 커버리지를 올렸을 때 하한을 올리고, 고친 부채 기준선 항목을 지우고, 줄어든 끄기 수의 기준선을 낮춘다.
- `pnpm tsx harness/src/run-all.ts --timing` — 검사마다 걸린 시간을 보여 준다.
- `pnpm quality write` — 품질 등급표(`docs/generated/quality.md`)를 새로 만든다. 손으로 고치지 않는다.
- `pnpm tsx harness/src/checks/docs.ts generate` — [결정 목록](../generated/decisions.md)과 [색인](../index.md)의 문서 목록을 한 번에 새로 만든다. 변경은 이 파일들을 커밋하지 않는다: 낡으면 알림만 나오고, 손으로 고치면 `DOC099`로 실패한다.
- `pnpm tsx harness/src/checks/docs.ts reverify <문서> --note "…"` — 문서가 낡은 대상 모두에 `docs/verified/` 기록을 쓴다. `--target <경로>`는 한 대상만 쓴다 (`DOC010`, `DOC011`).
- `pnpm tsx harness/src/checks/docs.ts seal-adr <ADR>` — 결정 기록을 봉인한다. 봉인한 뒤에는 본문을 고치지 못한다 (`DOC070`).
- `pnpm tsx harness/src/checks/docs.ts link-amends <ADR>` — 새 ADR의 `amends`가 가리키는 ADR에 `amended_by`를 남긴다 (`DOC091`).
- `pnpm tsx harness/src/checks/docs.ts audit-queue` — 본문이 그대로인 채 다시 확인만 된 문서를 오래 감사 못 받은 순으로 보여 준다. 읽은 뒤 `docs.ts audit <문서> --note "한 줄"`.
- `pnpm tsx harness/src/checks/licenses.ts write` — 제3자 고지 파일(`THIRD_PARTY_NOTICES.md`)을 새로 만든다. 손으로 고치지 않는다.
- `pnpm lock-tests` — 새 테스트를 잠금 파일에 추가한다. 기존 항목은 바꾸지 않는다.

## CI

`.github/workflows/ci.yml`이 합치기 관문이다. 필수 검사는 `gate` 하나다.

- 보통의 변경: `lint-types`(Biome와 타입), `test`(시험과 커버리지를 한 번에 돌리고, 그 커버리지로 구조 검사 전체를 돈다).
- 문서(`*.md`)만 바뀐 풀 리퀘스트: `docs`(구조 검사만).
- `.github/workflows/entropy.yml`은 아무도 올리지 않아도 한 주에 한 번 구조 검사를 돌린다. 문서는 나이로 낡고 부채는 기한이 온다.

## 저장소 호스트에서 켜야 하는 설정 (저장소 밖)

1. 기본 브랜치 보호: `gate` 통과 필수, 직접 push 금지.
2. "코드 소유자 승인 필수"를 쓸 때는 `.github/CODEOWNERS`의 소유자를 실제 사용자나 팀으로 둔다.

## 테라리움의 하네스와 맞추기

하네스는 테라리움의 복사본이라 두 저장소에서 따로 고쳐진다(TD-0001). 테라리움에서 하네스가 고쳐지면 그 변경을 보고 여기에 옮기고, 여기서 고친 것은 테라리움에도 알린다. 가져오지 않은 것(UX 측정, 화면 도우미 검사)은 ADR-0002에 적혀 있다.
