---
title: 릴리스 만들기 — 태그를 푸시하거나 Actions 화면에서 수동 실행하면 다섯 대상의 실행 파일이 GitHub 릴리스로 나가요
kind: guide
status: active
follows:
  - docs/design-docs/adr/0039-release-build-for-five-targets-with-a-reproducible-program.md
  - docs/design-docs/adr/0047-release-by-manual-run-with-version-and-commit.md
covers:
  - scripts/release-build.sh
  - scripts/release-smoke.sh
  - scripts/release-plan.sh
  - scripts/release-publish.sh
  - .github/workflows/release.yml
---
# 릴리스 만들기

빌드 환경이 없는 사람이 GitHub 릴리스에서 자기 운영체제용 파일 하나를 내려받아 실행할 수 있게 하는 절차예요. 무엇을 왜 이렇게 정했는지는 ADR-0039와 ADR-0047에 있어요. 이 문서는 운영자가 따라 하는 순서만 적어요.

## 1. 무엇이 나가요

태그 하나가 릴리스 하나예요. 릴리스에는 대상마다 압축 파일 하나와 `SHA256SUMS`(파일 이름 순서로 정렬)가 붙어요.

| 대상 | 파일 | 안에 든 것 |
|---|---|---|
| 리눅스 amd64, arm64 | `praxium_<버전>_linux_<arch>.tar.gz` | `praxium`, `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.md` |
| 맥 arm64, amd64 | `praxium_<버전>_darwin_<arch>.tar.gz` | 같음 |
| 윈도우 amd64 | `praxium_<버전>_windows_amd64.zip` | `praxium.exe`와 같은 문서 |

실행 파일에는 웹 UI가 들어 있어요. 받은 사람은 압축을 풀고 `praxium version`으로 버전을 보고 `praxium api serve ...`로 서버를 띄워요(ADR-0035, ADR-0034).

## 2. 버전 이름

시맨틱 버저닝이에요. 1.0 전에는 `0.x`이고 알파는 `v0.1.0-alpha.1`, `v0.1.0-alpha.2`처럼 써요. 태그에 `-`가 있으면 사전 릴리스로 표시돼요. 프로그램이 보이는 버전은 태그에서 `v`를 뺀 값이에요.

## 3. 로컬에서 만들어 보기

태그를 만들기 전에 같은 스크립트로 같은 파일을 만들 수 있어요. 변경을 모두 커밋한 작업 트리에서, Go와 pnpm이 있고 `pnpm install`을 마친 상태로 실행해요. GNU tar와 `zip`도 필요해요(리눅스 기준이고 맥은 `gnu-tar`가 필요해요).

```
scripts/release-build.sh --version 0.1.0-alpha.1 --out /tmp/praxium-release
(cd /tmp/praxium-release && sha256sum -c SHA256SUMS)
scripts/release-smoke.sh /tmp/praxium-release/praxium_0.1.0-alpha.1_linux_amd64.tar.gz
```

- `--out`의 폴더는 비어 있거나 없어야 해요. 기본은 `dist/release`예요(커밋되지 않아요).
- `release-smoke.sh`는 압축을 풀어 `praxium version`과 `--help`를 확인하고, `ingest init`으로 만든 예제를 `ingest inspect`와 `ingest run`(엑셀과 CSV)으로 수집한 뒤 `praxium local`로 띄워 API가 수집한 객체를 주는지 보고, 합성 세계(`praxium worldgen`)를 올린 개발 서버를 띄워 `/`가 웹 UI로 200을 주고 KG가 보이는지 봐요. 이 컴퓨터의 운영체제에 맞는 파일만 테스트할 수 있어요.
- 같은 커밋을 다시 빌드하면 같은 해시가 나와요(같은 Go 버전일 때). 두 번 만들어 `SHA256SUMS`를 비교해 보세요.
- 컴파일만 확인하려면 `scripts/release-build.sh --compile-only`예요. 웹도 압축도 하지 않고 평소 CI가 같은 명령을 돌려요.

## 4. 릴리스를 내는 두 방법

어느 쪽이든 같은 워크플로(`release`)가 돌고, 같은 다섯 파일과 `SHA256SUMS`가 릴리스에 올라가요. 릴리스할 수 있는 사람은 저장소에 쓰기 권한이 있는 사람이에요.

- **태그 푸시.** 터미널에서 `v*` 태그를 만들어 푸시해요(5절).
- **수동 실행.** GitHub의 Actions 화면에서 `release` 워크플로를 골라 "Run workflow"를 눌러요. 태그를 푸시할 수 없는 환경에서도, 터미널 없이도 돼요. 워크플로 파일이 기본 브랜치에 있어야 보여요.

### 수동 실행의 입력

| 입력 | 뜻 |
|---|---|
| `version` | 릴리스할 버전이에요. `0.1.0-alpha.1`도 `v0.1.0-alpha.1`도 돼요(태그는 언제나 `v`로 시작해요). 비우면 스냅숏 빌드만 하고 릴리스는 만들지 않아요. 채우면 그 버전으로 빌드하고 태그와 릴리스를 함께 만들어요. |
| `commit` | 릴리스할 커밋의 40자리 SHA예요. 비우면 실행한 브랜치의 끝 커밋이에요. |

`version`을 채웠을 때 `plan` 작업이 아래를 확인하고, 하나라도 어긋나면 이유를 밝히고 실패해요. 그러면 빌드도 릴리스도 일어나지 않아요.

- 워크플로를 메인 브랜치에서 실행했어요. 다른 브랜치나 태그에서 `version`을 채우면 실패해요. 게시 스크립트와 릴리스 노트는 실행한 곳의 것을 쓰므로, 검토를 거쳐 메인에 들어온 것만 쓰려는 규칙이에요(스냅숏은 어느 브랜치에서나 돼요).
- 버전이 시맨틱 버저닝이에요(`1.2.3` 또는 `1.2.3-alpha.1`).
- 커밋이 40자리 16진수이고 저장소에 있어요. 줄임 SHA와 브랜치 이름은 안 돼요.
- 커밋이 메인 브랜치에 있어요(메인의 끝 커밋이거나 그 조상). 메인에 없는 커밋은 릴리스할 수 없어요.
- 그 태그가 아직 없어요. 이미 있으면 다음 버전을 써요.
- 릴리스 전에 다시 확인해야 하는 문서가 모두 최신이에요(5절 1번, ADR-0056). 태그 푸시와 수동 릴리스는 같은 확인을 하고, 스냅숏은 하지 않아요.

빌드는 릴리스할 커밋의 스크립트로 하고, 릴리스 노트와 게시 스크립트는 워크플로를 실행한 메인 브랜치의 것을 써요(ADR-0047). 그래서 이미 검증한 옛 커밋을 그대로 릴리스할 수 있어요. 처음에는 `version`을 비우고 한 번 실행해 전체 경로(빌드, 압축, 실행 확인)가 러너에서 도는지 봐요.

```
gh workflow run release.yml --ref main
gh run watch
gh run download <실행 번호> -n release-files -D /tmp/praxium-artifact
```

스냅숏의 버전은 `0.0.0-snapshot.<커밋 앞 7자>`예요. 요약 화면(Summary)에 `SHA256SUMS`가 나오고, 아티팩트는 14일 동안 남아요.

### 릴리스 노트의 위치

릴리스 노트는 리포지토리의 `docs/releases/<태그>.md`예요(예: `docs/releases/v0.1.0-alpha.1.md`). 앞의 프런트매터를 뺀 본문이 릴리스 페이지의 노트가 되고, 끝에 `SHA256SUMS`가 코드 블록으로 붙어요. 노트는 메인에 병합하기 전에 검토를 받고, 메인에 파일이 있어야 쓰여요. 파일이 없으면 GitHub가 만든 노트를 쓰고 체크섬은 마찬가지로 붙어요.

## 5. 릴리스 내기

1. 릴리스 전에 개요를 요구사항의 변경과 대조해 실제로 읽어요. 제품 설계 개요가 요구사항을 따르는 관계는 다른 변경에서는 다시 확인하지 않고(보통 검사는 알림만 해요) 릴리스 때 한 번 확인해요(ADR-0056). 다음 명령이 낡은 관계와 확인 명령을 알려 줘요. 고칠 곳이 있으면 문서를 고치고, 메인에 병합한 뒤에 릴리스해요. 릴리스 검사는 워크플로를 실행한 ref(수동 릴리스는 메인 브랜치의 끝, 태그 푸시는 태그의 커밋)의 문서를 보기 때문이에요.

```
pnpm tsx harness/src/checks/docs.ts --release
pnpm tsx harness/src/checks/docs.ts reverify docs/design-docs/product-design.md --target docs/product-specs/requirements-<영역>.md --note "<무엇과 대조했고 무엇을 고쳤는지, 또는 왜 고칠 것이 없는지>"
```

   `--note`는 확인한 내용을 적는 자리예요. "그대로 둠" 같은 한 줄이 아니라, 지난 릴리스 이후 요구사항 문서(`requirements.md`와 `requirements-<영역>.md`)에서 어떤 `REQ`가 바뀌었는지 보고 개요의 어느 절과 대조했는지, 무엇을 고쳤는지(고칠 것이 없으면 그 이유)를 적어요.
2. 내려받으려는 커밋이 메인 브랜치에 있고 그 커밋의 CI(`gate`)가 통과했는지 확인해요.
3. 처음 한 번은 스냅숏 실행(4절)이 성공했는지 확인해요.
   MCP 인터페이스(`/mcp`)가 바뀐 릴리스에서는 `pnpm mcp:conformance`도 한 번 돌려요. 공식 적합성 도구(`@modelcontextprotocol/conformance`)가 이 컴퓨터에서 합성 세계를 올린 개발 서버를 띄워 테스트하고, 자기가 띄운 프로세스만 끝내요. 이 도구는 `pnpm check`에 들어 있지 않아요. `scripts/mcp-conformance-expected.yaml`에 없는 실패나 낡은 항목이 있으면 실패로 끝나요(ADR-0068, REQ-516).
4. `docs/releases/<태그>.md`에 릴리스 노트를 써서 메인에 병합해요(없어도 되지만 쓰기를 권해요). 노트에 서명을 아직 하지 않았다는 말을 적어요. 맥과 윈도우는 처음 실행할 때 확인되지 않은 개발자라는 경고가 뜰 수 있고, 내려받은 사람이 확인할 수 있는 것은 `SHA256SUMS`뿐이에요(ADR-0039).
5. 둘 중 하나로 릴리스해요.
   - 수동 실행: Actions 화면에서 `version`과 `commit`을 넣어 실행해요. 명령줄로는 `gh workflow run release.yml --ref main -f version=0.1.0-alpha.1 -f commit=<40자리 SHA>`예요.
   - 태그 푸시:

```
git switch main && git pull
git tag -a v0.1.0-alpha.1 -m "v0.1.0-alpha.1"
git push origin v0.1.0-alpha.1
```

6. `release` 워크플로가 도는 것을 지켜봐요(`gh run watch`). 끝나면 릴리스 페이지에 다섯 파일과 `SHA256SUMS`가 있어요. 수동 실행이면 GitHub가 그 커밋에 태그도 만들었어요. 수동 실행이 만든 태그는 태그 푸시 워크플로를 다시 일으키지 않으니 릴리스가 두 번 만들어지지 않아요.

받은 사람은 이렇게 확인해요.

```
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf praxium_0.1.0-alpha.1_linux_amd64.tar.gz
./praxium_0.1.0-alpha.1_linux_amd64/praxium version
```

내려받은 뒤 가장 쉬운 첫 단계는 `praxium --help`로 명령을 둘러보는 거예요. 그다음 `praxium ingest init -out ex`로 예제를 만들고, `praxium ingest run -data-dir data ...`로 수집한 뒤 `praxium local -data-dir data`로 열어요(ADR-0041).

## 6. 잘못됐을 때

- **`plan` 작업이 실패했어요.** 로그의 `release-plan:` 줄이 이유예요. 버전 형식, 커밋이 메인에 없음, 태그가 이미 있음, 릴리스 전에 다시 확인해야 하는 문서가 낡았음(`DOC013`) 중 하나예요. 마지막이면 로그가 어느 문서를 무엇에 대해 다시 확인해야 하는지와 명령을 말해요. 5절 1번대로 확인하고 고친 것을 메인에 병합한 뒤 다시 실행해요. 입력을 고쳐 다시 실행해요.
- **빌드 작업이 실패했어요.** 릴리스는 만들어지지 않아요. 원인을 고친 뒤 메인 브랜치에서 새 커밋에 새 태그를 만들거나, 같은 태그를 지우고 다시 만들어요(아래).
- **`release` 작업이 "태그가 있는지 확인하지 못했다"고 해요.** 네트워크나 GitHub 오류예요. 아무것도 게시되지 않으니 다시 실행해요.
- **`release` 작업이 "릴리스가 빌드한 커밋이 아니라 다른 커밋에 있다"고 해요.** 게시 직전에 같은 이름의 태그가 만들어진 경우예요. 안내된 대로 릴리스와 태그를 지우고(`gh release delete <태그> --cleanup-tag`) 다시 실행해요.
- **`release` 작업이 파일이 `SHA256SUMS`와 맞지 않는다고 해요.** 아무것도 게시되지 않아요. 빌드를 다시 실행해요.
- **태그를 잘못 만들었어요.** 릴리스가 있으면 `gh release delete <태그> --cleanup-tag`로 릴리스와 태그를 함께 지우고, 그 뒤에 5절 5번부터 다시 해요. 릴리스가 없으면 `git push origin :refs/tags/<태그>`와 `git tag -d <태그>`예요. 이미 누군가 내려받았을 수 있으니 공개한 태그는 되도록 지우지 말고 다음 번호로 내요.
- **버전이 시맨틱 버저닝이 아니라고 해요.** 태그(수동 실행이면 `version`)가 `v1.2.3` 또는 `v1.2.3-alpha.1` 형식이어야 해요.

## 7. 아직 하지 않는 것

서명, SBOM, 컨테이너 이미지, 빌드 출처 증명, 맥 공증과 윈도우 코드 서명, 패키지 관리자 배포는 오픈 베타 전에 해요(ADR-0039).

## 8. 고정한 액션 올리기

워크플로와 복합 액션은 바깥 액션을 태그가 아니라 40자리 커밋 해시로 참조해요(`uses: actions/checkout@<해시> # v4.4.0`). 태그는 나중에 다른 코드로 옮겨질 수 있어서예요. 검사 `WFL001`이 해시가 아닌 참조를 막아요(ADR-0064).

1. 새 버전이 필요하면 사람이 해시를 바꾸는 풀 리퀘스트를 열어요. 자동으로 올리는 도구는 쓰지 않아요. `.github/`는 보호 경로라 변경에 결정 기록이 필요해서, 자동 풀 리퀘스트는 검사를 통과하지 못해요.
2. 해시는 `git ls-remote --tags https://github.com/<소유자>/<이름>`로 확인해요. 주석 달린 태그는 `^{}`가 붙은 줄이 커밋이에요.
3. 해시와 줄 끝 주석의 버전을 함께 바꿔요. 주석의 버전은 검사하지 않으니 사람이 맞춰요.
