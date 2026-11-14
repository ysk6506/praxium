---
title: ADR-0035 임시 콘솔을 apps/web의 웹 UI로 만들고 실행 파일에 담아 제공한다
kind: decision
status: active
enforced_by:
  - ARCH010
  - ARCH011
  - ARCH020
  - WEB001
  - WEB010
  - WEB011
  - WEB013
  - WEB020
  - WEB021
  - WEB030
  - WEB031
  - WEB032
  - COV001
  - TRACE001
  - harness/src/checks/web.test.ts
  - harness/src/checks/test-unlocked.test.ts
  - apps/web/src/api/generator/generator.test.ts
  - apps/web/src/api/client.test.ts
  - apps/web/src/data/pure.test.ts
  - apps/web/src/ui/ui.test.tsx
  - backend/internal/httpapi/ui_test.go
  - backend/internal/webui/webui_test.go
updates_docs:
  - ARCHITECTURE.md
  - AGENTS.md
  - README.md
  - docs/product-specs/requirements.md
  - docs/design-docs/temporary-console.md
touches_protected:
  - .github/workflows/ci.yml
  - .gitignore
  - AGENTS.md
  - THIRD_PARTY_NOTICES.md
  - apps/web/package.json
  - biome.json
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
  - docs/product-specs/requirements.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/licenses.yaml
  - harness/config/test-lock.json
  - harness/config/tests.yaml
  - harness/config/web.yaml
  - harness/src/checks/test-unlocked.test.ts
  - harness/src/checks/test-units.ts
  - harness/src/checks/traceability.ts
  - harness/src/checks/web.test.ts
  - harness/src/checks/web.ts
  - harness/src/run-all.ts
  - package.json
  - vitest.config.ts
amends:
  - adr: "0016"
    what: 9번 임시 콘솔의 위치, 빌드, 제공 방식(빌드 없는 정적 파일과 기본 꺼짐을 apps/web의 빌드와 실행 파일에 포함으로)
decision_hash: e01ddabc80ee82e2
---
# ADR-0035 임시 콘솔을 apps/web의 웹 UI로 만들고 실행 파일에 담아 제공한다

## 맥락

제품 책임자가 브라우저에서 제품을 써 보려면 화면이 필요하다. ADR-0016의 9번은 임시 콘솔을 "빌드 없는 정적 파일, 실행 파일 밖의 폴더, 기본은 꺼짐, 토큰은 메모리에만"으로 정했다. 그 모양으로는 KG의 객체를 테이블로 보고 이웃을 따라가는 화면을 만들고 오래 고치기 어렵다. 타입 안전한 API 호출도 손으로 맞춰야 한다. 이 결정은 콘솔을 제대로 된 단일 페이지 앱으로 만들되, 지금까지 지킨 두 가지를 그대로 둔다. 백엔드는 화면 없이 빌드하고 테스트한다. 화면은 공개된 `/v1`만 부른다. 화면 쪽 용어는 용어집의 임시 콘솔을 쓴다.

## 결정

1. **위치.** 프런트엔드는 이 리포지토리의 `apps/web`에 둔다. ADR-0016 1번의 "화면 코드는 두지 않는다"와 9번의 콘솔 형태를 이 결정이 바꾼다. 콘솔은 여전히 임시이고, 프런트엔드 리포지토리가 서거나 클로즈드 베타가 끝나면 지운다(TD-0007). 지울 때는 `apps/web`, `backend/internal/webui`, 구조 계약의 `web.*` 유닛과 `web.yaml`, 의존성 표의 행을 함께 지운다.
2. **실행 파일에 담는다.** 빌드 결과를 Go의 `embed`로 `backend/internal/webui`(구조 계약의 `adapter.webui`)에 담고 `praxium api serve`가 `/v1` 밖의 GET에 정적 파일로 낸다. 외부 주소를 부르지 않으므로 폐쇄망에서 실행 파일 하나로 동작한다. 라우터는 주소의 `#` 뒷부분을 쓰므로 서버는 정적 파일과 `index.html`만 내면 된다. `/v1`로 시작하는 주소는 절대 화면으로 대체하지 않는다. 응답에는 같은 출처만 허용하는 `Content-Security-Policy`, `X-Frame-Options`, `nosniff`를 단다. `-web=false`로 끌 수 있고 기본은 켜짐이다.
3. **백엔드는 화면 없이 빌드된다.** 임베드 폴더에는 `placeholder.html` 하나만 커밋하고 실제 빌드 결과는 `.gitignore`로 제외한다. 백엔드의 어떤 Go 파일이나 모듈 파일도 `apps/web`을 가리키지 않는다. 빌드 스크립트(`scripts/build-with-web.sh`, `pnpm web:build`)가 웹 빌드, 복사, Go 빌드를 한 번에 한다.
4. **기술.** React 19, TypeScript, Vite의 정적 단일 페이지 앱이다. 스타일은 Tailwind CSS v4이고 색, 간격, 글꼴은 토큰 파일 하나(`apps/web/src/ui/tokens.css`)가 전부다. Tailwind의 기본 테마는 가져오지 않으므로 토큰에 없는 색은 존재하지 않는다. 동작(대화상자, 탭, 선택 상자, 체크 박스)은 Base UI(`@base-ui/react`)가 맡는다. 라우터, 상태 관리, 데이터 조회, 아이콘, 테이블 라이브러리는 쓰지 않는다.
5. **계층.** 위로만 의존한다: `src/api`(`web.api`), `src/data`(`web.data`), `src/ui`(`web.ui`), `src/screens`(`web.screens`), 나머지(`web.app`).
   - `api`는 OpenAPI 문서에서 자동 생성한 타입과 호출 함수다. 생성기는 의존성 없는 작은 스크립트다. 생성 파일의 머리말에 계약의 해시와 본문의 해시가 있고, 계약이 바뀌었는데 다시 생성하지 않았거나 손으로 고치면 검사가 실패한다.
   - `data`는 조회, 로딩과 오류 상태, 페이지네이션이다. API의 오류 형식(`problem+json`)을 읽을 수 있는 한국어 문장으로 바꾼다.
   - `ui`는 프락시움이 이름 붙인 컴포넌트(`Button`, `Table`, `Dialog` 등)이다. Base UI는 여기서만 import한다.
   - `screens`는 `ui`와 `data`만 쓴다. Base UI와 색 리터럴은 쓸 수 없다.
6. **검사.** 기존 규칙은 하나도 느슨하게 하지 않는다. 새 검사는 모두 기존 `architecture` 검사 안에서 돈다(검사 이름은 잠긴 테스트가 정한다).

   | 규칙 | 내용 | 코드 |
   |---|---|---|
   | (가) | 화면과 데이터 계층은 `@base-ui/react`를 import할 수 없다. 구조 계약의 `third_party`가 `web.ui`에만 허용한다 | `ARCH020` |
   | (나) | 토큰 파일 밖의 색 리터럴(`#hex`, `rgb()`, `hsl()`, `oklch()` 등)과 임의 값이나 기본 팔레트 이름의 Tailwind 색 | `WEB020`, `WEB021` |
   | (다) | `apps/web/package.json`의 의존성은 `harness/config/web.yaml`의 허용 목록에 있어야 한다 | `WEB001` |
   | (라) | 생성한 API 코드는 계약과 같아야 한다: 계약 해시, 본문 해시, 생성기를 다시 돌린 결과 | `WEB010`, `WEB011`, `WEB012`, `WEB013` |
   | (마) | 백엔드가 `apps/web` 없이 빌드된다: 자리표시자 페이지, 자리표시자 외 산출물의 제외, Go 코드가 `apps/web`을 가리키지 않음 | `WEB030`, `WEB031`, `WEB032` |

   구조 계약에 `web.testsupport`, `web.api`, `web.data`, `web.ui`, `web.screens`, `web.build`, `web.app`(TypeScript)와 `adapter.webui`(Go) 유닛을 등록한다. `apps/web`의 설정 파일은 `apps/web/config`로 두어 `web.build` 유닛에 속하게 한다. Biome는 Tailwind v4의 `@theme`를 알지 못하므로 토큰 파일 하나에서만 `noUnknownAtRules`를 끈다.
7. **테스트 잠금의 범위.** `src/api`(생성기 포함), `src/ui`, `src/data`의 테스트는 다른 테스트와 같이 잠그고 요구사항을 추적한다. `src/screens`는 잠금과 요구사항 추적에서 뺀다. 임시 화면은 제품 책임자가 보고 나서 자주 바뀌기 때문이다. 설정에는 `tests.yaml`의 `unlocked_globs`로 나타나고, 정적 테스트 규칙(건너뛰기 금지 등)은 그대로 적용된다. 커버리지 하한도 같은 범위(`api`, `data`, `ui`)이고 화면은 제외한다. 화면은 실제 서버와 합성 세계로 브라우저에서 확인한다.
8. **화면의 말과 모양.** 화면의 문구는 한국어이고 용어집의 표준 용어를 쓴다(근거 등급은 사실, 가정, 알 수 없음). 글꼴은 시스템 글꼴 묶음이고 한국어 글꼴을 라틴 글꼴보다 앞에 두며 내려받지 않는다. 줄 간격을 넓히고 `word-break: keep-all`을 쓴다. 어두운 모드는 토큰을 바꿔 시스템 설정을 따른다. 모든 조작은 키보드로 된다.
9. **범위.** 이 변경의 화면은 임시이고 다음뿐이다: 접속 설정(개발용 토큰을 브라우저에 저장하고 API 호출에 싣는다), KG 선택, 객체 타입 목록과 타입별 객체 테이블(`queries:find`, 페이지네이션, 속성 컬럼, 식별자), 객체 상세(`queries:get`: 속성, 식별자, 근거 등급과 출처), 이웃(`queries:neighbors`: 링크 타입별 목록과 이동). 그래프 그림은 없다. 로딩, 빈 상태, 오류 상태를 모두 보여 준다. 화면이 호출하는 경로는 공개된 `/v1`뿐이고 콘솔 전용 경로는 없다.

### 도입하는 패키지

런타임 의존성은 `react`, `react-dom`, `@base-ui/react` 셋뿐이다. 전이 개수는 `pnpm-lock.yaml`의 `dependencies`를 끝까지 따라간 수다. 모두 MIT이고 허용 목록(`harness/config/licenses.yaml`)에 있다. 번들에 들어가는 서드파티 패키지는 모두 `THIRD_PARTY_NOTICES.md`에 있다.

| 이름 | 버전 | 라이선스 | 전이 의존 | 이유 |
|---|---|---|---|---|
| `react` | 19.3.1 | MIT | 0 | 화면을 선언적으로 그린다. 직접 만들면 갱신과 키보드 포커스 관리를 다시 짜야 한다 |
| `react-dom` | 19.3.1 | MIT | 2 (`react`, `scheduler`) | React를 브라우저에 그린다 |
| `@base-ui/react` | 1.9.0 | MIT | 13 (실행 시 번들에는 `@floating-ui`, `@base-ui/utils`, `reselect` 등 9개, 나머지는 타입) | 대화상자, 탭, 선택 상자의 키보드 조작과 접근성 동작은 직접 만들기 어렵고 오래 유지하기 어렵다. 스타일이 없는 동작 부품이라 토큰만으로 꾸민다 |
| `tailwindcss` | 4.3.5 | MIT | 0 (개발용) | 토큰 하나를 스타일의 전부로 만들고 쓰지 않는 스타일을 빌드에서 뺀다 |
| `@tailwindcss/vite` | 4.3.5 | MIT | 108 (개발용. `vite`와 겹치는 것 포함) | Tailwind를 Vite 빌드에 잇는다 |
| `vite` | 7.3.8 | MIT | 83 (개발용) | 정적 번들을 만든다. 테스트 도구(`vitest`)가 이미 쓰는 같은 버전이라 새 사본이 늘지 않는다 |
| `happy-dom` | 21.0.4 | MIT | 8 (개발용) | 훅과 컴포넌트 테스트용 DOM. 테스트 파일마다 주석으로 고른다 |
| `@types/react`, `@types/react-dom` | 19.3.0 | MIT | 1, 2 (개발용) | 타입 검사 |

`@vitejs/plugin-react`는 넣지 않는다. esbuild의 JSX 변환으로 충분하고 저장하자마자 화면에 반영하는 개발 중의 기능(HMR)만 포기한다.

## 이유

- 한 리포지토리에 두면 계약의 변경이 생성기와 검사를 거쳐 화면의 타입 오류로 바로 드러난다. 따로 두면 두 곳의 버전을 맞추는 일이 생기고, 지금은 화면을 쓰는 사람이 제품 책임자 한 명이다.
- 실행 파일에 담으면 배포가 파일 하나이고 같은 출처라 CORS도 필요 없다. 폐쇄망에서 외부 CDN이나 글꼴을 부르지 않는다.
- 임베드 폴더에 자리표시자만 커밋하면 백엔드를 기여하는 사람이 Node를 설치하지 않아도 빌드와 테스트가 된다.
- 색을 한 파일에 모으면 어두운 모드와 이후 디자인 교체가 그 파일의 일이 된다. 기계가 막지 않으면 곧 임의의 색이 퍼진다.
- 화면은 보고 바꾸는 일이 많다. 잠금은 바꾸기 어렵게 만드는 장치이므로 그 비용이 이득보다 큰 곳에서만 뺀다. 데이터 조회와 오류 해석 같은 논리는 `data`와 `ui`에 두어 잠그고 측정한다.

## 버린 대안

- **빌드 없는 정적 파일 유지(ADR-0016 9번 그대로).** 표, 페이지네이션, 대화상자, 키보드 조작, 타입 안전한 호출을 직접 짜야 하고 오래 유지하기 어렵다.
- **프런트엔드 리포지토리를 지금 만들기.** 제품 책임자가 미뤘다. 폐쇄망 배포에 파일이 하나 더 필요해진다.
- **임베드 없이 디스크의 폴더를 읽기.** 실행 파일 하나라는 배포 조건을 깬다.
- **빌드 결과를 커밋하기.** 이진에 가까운 큰 파일이 리포지토리에 쌓이고 검토할 수 없다.
- **라우터, 상태 관리, 데이터 조회, 표, 아이콘 라이브러리.** 이 범위에서는 해시 라우터와 작은 훅으로 충분하고 의존성마다 라이선스와 전이 의존을 감시해야 한다.
- **화면까지 잠그기.** 화면이 바뀔 때마다 잠금을 고치는 ADR이 필요해진다.

## 결과

- `apps/web`이 생기고 구조 계약에 유닛과 허용 패키지가 등록된다. 새 검사 `WEB*`가 `architecture` 검사 안에서 돈다.
- `ci.yml`의 `test` 작업에 계약 일치 확인과 웹 포함 빌드가 더해진다.
- 객체 테이블의 속성 컬럼은 `queries:find`가 속성을 주지 않아 행마다 `queries:get`을 부른다(동시 6개). 목록 응답이 속성을 줄 수 있게 되면 `data`의 한 함수만 바꾼다.
- 개발용 토큰은 브라우저의 `localStorage`에 저장한다. 임시 콘솔 문서의 옛 규칙(토큰은 메모리에만, 입력란 없음)을 이 결정이 대체한다. 개발용 인증이 실제 인증(TD-0010)으로 바뀌면 이 화면의 접속 설정을 로그인 흐름으로 바꾼다.
- 프로덕션 모드에서 화면을 낼지는 제품 책임자가 정한다. 지금은 `-web=false`로 끈다.
