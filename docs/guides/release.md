---
title: 릴리스 만들기 — 태그를 푸시하면 다섯 대상의 실행 파일이 GitHub 릴리스로 나가요
kind: guide
status: active
follows:
  - docs/design-docs/adr/0039-release-build-for-five-targets-with-a-reproducible-program.md
covers:
  - scripts/release-build.sh
  - scripts/release-smoke.sh
  - .github/workflows/release.yml
---
# 릴리스 만들기

빌드 환경이 없는 사람이 GitHub 릴리스에서 자기 운영체제용 파일 하나를 내려받아 실행할 수 있게 하는 절차예요. 무엇을 왜 이렇게 정했는지는 ADR-0039에 있어요. 이 문서는 운영자가 따라 하는 순서만 적어요.

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
- `release-smoke.sh`는 압축을 풀어 `praxium version`과 `--help`를 확인하고, `ingest init`으로 만든 예제를 `ingest inspect`와 `ingest run`(엑셀과 CSV)으로 수집한 뒤 `praxium local`로 띄워 API가 수집한 객체를 주는지 보고, 서버와 `praxium demo`를 차례로 띄워 `/`가 웹 UI로 200을 주는지 봐요. 이 컴퓨터의 운영체제에 맞는 파일만 테스트할 수 있어요.
- 같은 커밋을 다시 빌드하면 같은 해시가 나와요(같은 Go 버전일 때). 두 번 만들어 `SHA256SUMS`를 비교해 보세요.
- 컴파일만 확인하려면 `scripts/release-build.sh --compile-only`예요. 웹도 압축도 하지 않고 평소 CI가 같은 명령을 돌려요.

## 4. 첫 태그 전에 수동으로 실행하기

릴리스 워크플로는 수동 실행(`workflow_dispatch`)도 돼요. 태그를 만들지 않고, 릴리스도 만들지 않고, 같은 빌드와 압축과 실행 확인을 러너에서 돌린 뒤 파일을 워크플로 아티팩트로만 남겨요. 워크플로 파일이 기본 브랜치에 있어야 실행할 수 있으니 병합한 뒤에 해요.

```
gh workflow run release.yml --ref main
gh run watch
gh run download <실행 번호> -n release-files -D /tmp/praxium-artifact
```

요약 화면(Summary)에 `SHA256SUMS`가 나오고, 아티팩트는 14일 동안 남아요. 이때 버전은 `0.0.0-snapshot.<커밋 앞 7자>`예요.

## 5. 릴리스 내기

1. 내려받으려는 커밋이 메인 브랜치에 있고 그 커밋의 CI(`gate`)가 통과했는지 확인해요.
2. 수동 실행(4절)이 성공했는지 확인해요(처음 한 번은 꼭 해요).
3. 태그를 만들어 푸시해요.

```
git switch main && git pull
git tag -a v0.1.0-alpha.1 -m "v0.1.0-alpha.1"
git push origin v0.1.0-alpha.1
```

4. `release` 워크플로가 도는 것을 지켜봐요(`gh run watch`). 끝나면 릴리스 페이지에 다섯 파일과 `SHA256SUMS`가 있어요. 릴리스 노트는 GitHub가 만든 것이고 필요하면 페이지에서 고쳐요.
5. 릴리스 노트에 서명을 아직 하지 않았다는 말을 적어요. 맥과 윈도우는 처음 실행할 때 확인되지 않은 개발자라는 경고가 뜰 수 있고, 내려받은 사람이 확인할 수 있는 것은 `SHA256SUMS`뿐이에요(ADR-0039).

받은 사람은 이렇게 확인해요.

```
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf praxium_0.1.0-alpha.1_linux_amd64.tar.gz
./praxium_0.1.0-alpha.1_linux_amd64/praxium version
```

내려받은 뒤 가장 쉬운 첫 단계는 `praxium demo`예요. 가상의 데이터로 서버를 띄우고, 토큰이 들어 있는 링크를 출력해요. 링크를 브라우저로 열면 되고 Ctrl+C로 끝내요. 그다음에는 `praxium --help`로 명령을 둘러봐요. 자기 파일을 넣어 보려면 `praxium ingest init -out ex`로 예제를 만들고, `praxium ingest run -data-dir data ...`로 수집한 뒤 `praxium local -data-dir data`로 열어요(ADR-0041).

## 6. 잘못됐을 때

- **빌드 작업이 실패했어요.** 릴리스는 만들어지지 않아요. 원인을 고친 뒤 메인 브랜치에서 새 커밋에 새 태그를 만들거나, 같은 태그를 지우고 다시 만들어요(아래).
- **태그를 잘못 만들었어요.** 릴리스가 있으면 `gh release delete <태그> --cleanup-tag`로 릴리스와 태그를 함께 지우고, 그 뒤에 5절 3번부터 다시 해요. 릴리스가 없으면 `git push origin :refs/tags/<태그>`와 `git tag -d <태그>`예요. 이미 누군가 내려받았을 수 있으니 공개한 태그는 되도록 지우지 말고 다음 번호로 내요.
- **버전이 시맨틱 버저닝이 아니라고 해요.** 태그가 `v1.2.3` 또는 `v1.2.3-alpha.1` 형식이어야 해요.

## 7. 아직 하지 않는 것

서명, SBOM, 컨테이너 이미지, 빌드 출처 증명, 맥 공증과 윈도우 코드 서명, 패키지 관리자 배포는 오픈 베타 전에 해요(ADR-0039).
