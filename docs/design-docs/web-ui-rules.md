---
title: 웹 UI 규칙 — 임시 콘솔의 화면 코드를 짤 때 지키는 것
kind: guide
status: active
covers:
  - apps/web/src/ui/tokens.css
  - apps/web/config/vite.config.ts
  - harness/config/web.yaml
  - harness/src/checks/web-rules.ts
  - harness/src/checks/web-colors.ts
  - backend/internal/httpapi/ui.go
follows:
  - docs/design-docs/adr/0035-web-ui-in-apps-web-embedded-in-the-program.md
  - docs/design-docs/adr/0045-web-ui-rules-from-the-chrome-web-guidance.md
---
# 웹 UI 규칙

`apps/web`의 화면 코드(HTML, CSS, TypeScript)를 쓰거나 고치기 전에 읽는다. 구글 크롬 팀의 웹 플랫폼 가이드 `GoogleChrome/modern-web-guidance-src`의 `guides/`에서 프락시움에 해당하는 것만 추려 규칙으로 만들었다(출처는 맨 끝). 기계가 확인할 수 있는 규칙은 하네스 검사가 지키고(`WEB040`~`WEB045`), 나머지는 쓰는 쪽이 지킨다. 계층, 색 토큰, 의존성 규칙은 [ADR-0035](adr/0035-web-ui-in-apps-web-embedded-in-the-program.md)와 [임시 콘솔](temporary-console.md)이 정한 그대로이고 이 문서는 그 위에 더해진다. 이 문서에 없는 패턴이 필요하면 가이드 저장소의 `guides/` 아래 해당 가이드를 열어 읽는다(읽기만 한다. 아래 "채택하지 않는 것" 참고).

## 1. 브라우저 지원 기준

| 구분 | 기준 |
|---|---|
| 기본 | Baseline Widely available(주요 브라우저 엔진 모두에서 30개월 넘게 쓸 수 있는 기능)만 폴백 없이 쓴다 |
| Newly available | 기능 감지(`@supports`, `CSS.supports`, 객체 존재 확인)와 점진 향상일 때만 쓴다. 없어도 같은 일이 되어야 하고 핵심 동작(이동, 표, 폼, 대화 상자)에는 쓰지 않는다. 폴백은 20줄 이하이고 의존성이 없어야 한다 |
| Limited(일부 엔진만), 오리진 트라이얼, 실험 플래그 | 쓰지 않는다 |
| 하한 | 크롬·엣지 111, 파이어폭스 128, 사파리 16.4. Tailwind CSS v4가 문서로 요구하는 최소 버전이고, 빌드 결과(`@property`, `color-mix()`, `@layer`)가 실제로 이 선에 의존한다 |

이유. 프락시움은 셀프 호스팅하는 오픈소스라서 사용자의 브라우저를 고를 수 없고, 기업 환경은 크롬과 엣지가 많아도 업데이트가 느릴 수 있다. 그래서 가장 새 기능이 아니라 Tailwind가 이미 요구하는 하한에서 멈춘다. 하한은 날짜가 지나면 낡으므로 숫자보다 위의 Baseline 규칙이 우선이고, 숫자는 Tailwind를 올릴 때 다시 본다([공식 호환성 문서](https://tailwindcss.com/docs/compatibility)).

빌드 대상은 Vite의 `build.target`(`apps/web/config/vite.config.ts`)에 이 하한을 그대로 적는다: `chrome111`, `edge111`, `firefox128`, `safari16.4`. 예전의 `es2022`는 언어 판 이름이라 브라우저 하한과 맞는지 알 수 없었다. TypeScript의 `target`과 `lib`(ES2023)는 타입 검사용이고 이 하한의 브라우저가 모두 지원하는 범위라서 그대로 둔다.

`<search>`처럼 Baseline이 된 지 얼마 안 된 요소는 모르는 브라우저에서 이름 없는 요소처럼 그려질 뿐이므로 클래스로 레이아웃을 주고 의미는 덧붙임으로만 쓴다.

## 2. 문서 기본과 내비게이션

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| `index.html`에 `lang="ko"`, `viewport`(확대를 막지 않는다), `color-scheme` 메타를 둔다 | `guides/html/html`, `guides/visual-design/dark-mode` | 쓰는 쪽 |
| 화면마다 `document.title`을 `"<대상> · <종류> · 프락시움"` 형식으로 갱신한다. 화면은 `useScreenTitle(titleOf(...))`로 이름을 알린다 | `guides/accessibility/accessibility`(WCAG 2.4.2) | `apps/web/src/data/screen.test.tsx` |
| 사용자가 링크나 버튼으로 화면을 옮기면 포커스를 새 화면의 `h1`로 옮긴다. 같은 화면 안의 쿼리 변경(탭, 검색, 정렬, 페이지)에서는 옮기지 않는다 | 같은 곳(WCAG 2.4.3) | 같은 시험 |
| 뒤로 가기와 앞으로 가기에서는 포커스를 두고 `aria-live="polite"` 영역으로 도착한 화면의 이름을 알린다 | 같은 곳(WCAG 4.1.3) | 같은 시험 |
| 화면마다 `h1` 하나, 구역은 `h2`, 단계를 건너뛰지 않는다. `main`은 하나이고 `nav`가 여럿이면 `aria-label`로 구별한다 | `guides/accessibility/accessibility` | 쓰는 쪽 |
| 이동은 `<a href>`, 동작은 `<button type="button">`. `div`에 `onClick`을 달지 않는다 | `guides/html/html` | Biome의 접근성 규칙 |
| 이동 경로는 `<nav aria-label>` 안의 `<ol>`이다 | `guides/ui-components/breadcrumbs` | `PageHeader` |
| 본문으로 건너뛰기 버튼이 첫 탭 대상이다 | `guides/accessibility/accessibility` | `AppShell` |

## 3. 접근성

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| 포커스 표시를 지우지 않는다. `outline-none`을 쓰면 같은 요소에 `focus-visible:` 대체 표시를 둔다 | `guides/accessibility/accessibility` | `WEB043` |
| 글자는 배경과 4.5:1 이상, 컨트롤의 테두리와 포커스 링은 3:1 이상(라이트와 다크 모두). 새 색 쌍은 `harness/config/web.yaml`의 `contrast`에 적는다 | `guides/visual-design/color`(WCAG 1.4.3, 1.4.11) | `WEB040` |
| 입력란, 선택 상자, 체크 박스, 버튼의 테두리는 `border-control-line`이다. `border-line`과 `border-line-strong`은 장식(구분선, 빈 상태 틀)에만 쓴다 | 같은 곳 | `WEB040`(토큰 값) |
| 색만으로 상태를 나타내지 않는다. 배지와 상태에는 글자나 모양을 함께 둔다 | 같은 곳(WCAG 1.4.1) | 쓰는 쪽 |
| 움직이는 것은 `motion-safe:`(또는 `prefers-reduced-motion: no-preference` 안)에서만 켠다. 색 전환은 둬도 된다. 전역으로 `animation-duration: 0.01ms`를 쓰지 않는다 | `guides/ui-behaviors/motion`(WCAG 2.3.3) | `WEB044` |
| 강제 색상 모드(`forced-colors: active`)에서는 배경이 사라진다. 배경색으로만 나타낸 상태(현재 메뉴, 선택한 탭, 배지)는 밑줄이나 테두리를 함께 둔다 | `guides/visual-design/color` | Playwright 확인(ADR-0045) |
| 글자가 든 요소의 높이를 고정하지 않는다(`h-8` 대신 `min-h-8`). 글자 크기는 `rem`, 줄 간격은 단위 없는 숫자이다. 200% 확대, 320px 폭, 글자 간격 조정(줄 간격 1.5, 글자 간격 0.12em, 단어 간격 0.16em, WCAG 1.4.12)에서 잘리지 않아야 한다 | `guides/visual-design/typography`, `guides/accessibility/respect-os-text-scale` | Playwright 확인 |
| 컨트롤의 크기는 24×24 CSS px 이상이다 | `guides/accessibility/accessibility`(WCAG 2.5.8) | 쓰는 쪽 |
| `tabindex`는 0이나 -1만 쓴다(양수 금지). `-1`은 포커스를 코드가 옮기는 대상(`main`, `h1`)에만 쓴다 | 같은 곳 | Biome |
| `aria-hidden`을 포커스할 수 있는 요소에 달지 않는다. 중복 ARIA를 달지 않는다 | 같은 곳 | Biome |

## 4. 폼과 오류

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| 모든 입력에 보이는 `<label>`을 연결하고, 도움말은 설명으로 연결한다. 자리표시자를 라벨로 쓰지 않는다(`TextField`) | `guides/forms/forms` | 쓰는 쪽 |
| 검색과 필터는 `<search>`로 감싸고 `type="search"`를 쓴다. 식별자나 이름을 치는 입력에는 `spellCheck={false}`와 `autoComplete="off"`를 둔다 | `guides/forms/forms`, `guides/html/html` | 쓰는 쪽 |
| 검증은 사용자가 입력을 마친 뒤(`blur`)와 제출 때 한다. 입력 중에는 오류를 지우기만 한다. 제출에 실패하면 첫 오류로 포커스를 옮긴다. 제출 버튼을 미리 비활성화하지 않는다 | `guides/forms/validate-input-after-interaction`, `guides/accessibility/accessible-error-announcement` | 쓰는 쪽 |
| `Enter`로 제출하거나 실행하는 입력은 입력기 조합 중(`event.isComposing`, Safari에서는 `keyCode === 229`)의 `Enter`를 무시한다. 한국어는 `Enter`로 글자를 확정한다 | `guides/forms/ime-safe-enter-submit` | 쓰는 쪽 |
| 오류는 `role="alert"`, 읽는 중은 `<output>`으로 알린다. 읽는 중을 긴급 알림으로 쓰지 않는다 | `guides/accessibility/accessibility` | `ErrorState`, `LoadingState` |

## 5. 테이블, 대화 상자, 탭

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| 테이블에는 이름(`aria-label` 또는 `caption`)을 주고 열 머리글은 `th scope="col"`이다. 레이아웃에 표를 쓰지 않는다 | `guides/ui-atoms/responsive-table` | `Table` |
| 가로로 스크롤되는 테이블은 키보드로 닿아야 한다: 넘칠 때 스크롤 영역에 `tabIndex={0}`, `role="region"`, 이름을 준다 | 같은 곳(WCAG 2.1.1) | `Table`의 넘침 시험 |
| 정렬하는 열은 `th` 안에 `button`을 두고 정렬 중인 열에 `aria-sort`를 단다. 정렬하지 않는 열에는 `aria-sort="none"`을 달지 않는 것이 가이드의 권고이지만 `SortTh`는 지금 달고 있다(잠긴 시험이 단언한다, ADR-0045의 보류) | `guides/ui-behaviors/sortable-table` | 쓰는 쪽 |
| 대화 상자는 `ui`의 `Dialog`(Base UI)를 쓴다. 포커스 가두기와 `Esc`, 닫을 때 포커스 복귀를 직접 짜지 않는다. 닫기 수단이 눈에 보여야 한다 | `guides/ui-components/dialog` | `Dialog` 시험 |
| 탭은 같은 화면 안의 보기 전환에만 쓰고 화면 이동에는 쓰지 않는다 | `guides/ui-components/tabs` | 쓰는 쪽 |

## 6. CSS

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| 색, 간격, 글꼴은 토큰이다. 색은 `tokens.css`에만 쓴다 | `guides/css/design-token-reactivity` | `WEB020`, `WEB021` |
| 레이어 순서는 `@layer theme, base, components, utilities`로 선언한다. 선택자 경쟁은 `@layer`와 `:where()`로 줄이고 BEM을 만들지 않는다 | `guides/css/css` | 쓰는 쪽 |
| 레이아웃은 flex와 grid를 쓰고, 부모 크기에 맞출 때는 컨테이너 쿼리를 쓴다. 뷰포트 높이는 `vh`가 아니라 `dvh`를 쓴다(새로 쓰는 코드부터) | `guides/css/responsive-design` | 쓰는 쪽 |
| 인라인 `style` 속성으로 디자인하지 않는다. 값이 계산에서 나올 때만 쓴다 | `guides/html/html` | 쓰는 쪽 |
| 새 CSS 기능은 1절의 기준을 따른다. `@property`, `color-mix()`, `oklch()`, `:where()`, `:has()`, `@layer`는 쓸 수 있고, `corner-shape`, `masonry`, 앵커 위치 지정, `appearance: base-select`, 스크롤 연동 애니메이션은 쓰지 않는다 | `guides/css/*` | 쓰는 쪽 |
| 어두운 모드는 `color-scheme: light dark`와 `prefers-color-scheme`으로 토큰 값만 바꾼다 | `guides/visual-design/dark-mode` | `WEB040`(양쪽 테마) |

## 7. 성능

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| 번들은 gzip으로 JS 170000바이트, CSS 10000바이트 이하이다(2026-11 측정은 JS 약 143000, CSS 약 6000). 예산을 올리려면 이유를 적은 ADR이 필요하다 | `guides/performance/performance` | `WEB045` |
| 스크립트는 `type="module"`이고 CSS `@import`는 빌드 입력에서만 쓴다. 쓰지 않는 코드는 트리 셰이킹으로 뺀다 | 같은 곳 | Vite 빌드 |
| 한 번에 50ms를 넘기는 일을 만들지 않는다. 큰 목록은 페이지네이션으로 자른다 | `guides/performance/break-up-long-tasks` | 쓰는 쪽 |
| 지연 로딩(`import()`)은 번들이 예산에 다가올 때 화면 단위로 도입한다. 지금은 번들 하나이다 | `guides/performance/performance` | `WEB045` |
| 페이지에 `Cache-Control: no-store`를 쓰지 않는다(뒤로 가기 캐시가 막힌다). 해시가 붙은 정적 파일은 `immutable` | 같은 곳 | `ui_headers_test.go` |

## 8. 보안

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| `dangerouslySetInnerHTML`, `innerHTML`·`outerHTML` 대입, `insertAdjacentHTML`, `setHTMLUnsafe`, `document.write`, `eval`, `new Function`, 문자열 `setTimeout`을 쓰지 않는다. API가 준 값은 텍스트로만 그린다 | `guides/security/security`, `guides/security/trusted-types` | `WEB041` |
| `apps/web/src`와 `index.html`에 `http://`·`https://` 절대 주소를 쓰지 않는다. 같은 출처의 경로(`/v1/...`)만 부른다. 외부 글꼴, 스크립트, 이미지, 분석 도구를 넣지 않는다 | `guides/security/restrict-outbound-connections`, `guides/security/secure-external-subresources` | `WEB042` |
| 화면 응답의 헤더는 `backend/internal/httpapi/ui.go`의 한 곳에서만 정한다: CSP(`default-src`, `script-src`, `connect-src`, `worker-src`가 `'self'`, `object-src 'none'`, `base-uri 'none'`, `frame-ancestors 'none'`, `form-action 'self'`), `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, `Cross-Origin-Opener-Policy: same-origin` | `guides/security/security` | `ui_headers_test.go` |
| 인라인 스크립트를 두지 않는다(`script-src 'self'`로 충분하게). `style-src 'unsafe-inline'`은 Base UI가 위치를 계산해 `style` 속성을 쓰기 때문에 열어 두었고 스크립트 실행과 무관하다 | 같은 곳 | `ui_headers_test.go` |
| 토큰 같은 시크릿을 URL의 `?`나 로그에 남기지 않는다. 접속 링크는 `#` 뒤에만 싣는다 | 같은 곳 | ADR-0038 |

## 9. 국제화(한국어)

| 규칙 | 근거 가이드 | 확인 |
|---|---|---|
| 글꼴은 시스템 글꼴이고 한국어 글꼴을 라틴 글꼴보다 앞에 둔다. 내려받지 않는다 | `guides/performance/performance`(글꼴 절) | `tokens.css` |
| 줄바꿈은 `word-break: keep-all`과 `overflow-wrap: break-word`이다. 식별자와 경로는 `break-all`이다 | `guides/visual-design/typography` | `app.css` |
| 줄 간격은 한국어에 맞춰 1.5 이상이다 | 같은 곳 | `tokens.css` |
| 숫자와 시각은 `data/format.ts`의 함수로만 보인다. 사용자 시간대로 바꾸는 요구가 생기면 `Intl`을 쓴다 | `guides/js/js` | `pure.test.ts` |
| 문구는 용어집의 표준 용어이다 | [용어집](glossary.md) | `WORD*` |

## 10. 채택하지 않는 것

| 가이드의 권고 | 이유 |
|---|---|
| `npx modern-web-guidance`(검색, 가져오기, 설치)와 스킬을 에이전트가 먼저 실행하게 하는 방식 | 네트워크가 필요하고 설치 횟수, 에이전트 이름, 검색어를 외부로 보내는 텔레메트리가 있다. 폐쇄망과 의존성 최소 원칙에 맞지 않는다. 필요한 가이드는 이 문서가 추려 두었고 나머지는 사람이나 에이전트가 읽기만 한다 |
| 의존성을 늘리는 권고(`web-features` 데이터, 접근성 자동 검사 도구, 폴리필, 라우터, 애니메이션, 아이콘 라이브러리) | 런타임 의존성은 ADR-0035의 셋뿐이다. 새 의존성은 ADR, 라이선스, 전이 수를 거친다(`WEB001`) |
| 외부 서비스에 의존하는 권고(서드파티 스크립트, 분석, 외부 글꼴, `preconnect`, SRI, `Connection-Allowlist`) | 외부 요청을 하지 않는다(8절) |
| 서버 렌더링을 전제한 권고(nonce 기반 CSP, 스트리밍, 서버 측 캐시) | 정적 단일 페이지 앱이고 서버는 HTML을 만들지 않는다 |
| 서비스 워커, PWA, 오프라인 | 서버가 항상 필요한 도구이고 캐시 갱신 문제만 는다 |
| 네이티브 `<dialog>`와 `popover`로 Base UI 교체 | 탭, 선택, 체크 박스에도 Base UI를 쓰고 ADR-0035가 정당화했다. 정식 화면을 다시 만들 때 따로 판단한다 |
| `require-trusted-types-for 'script'` | 위험한 싱크를 `WEB041`이 막는다. React와 Base UI 내부가 호환되는지 검증하지 못했다. 정식 화면에서 다시 본다 |
| 쿠키 접두사, `SameSite`, HSTS, `Clear-Site-Data` | 쿠키 인증이 없고 TLS를 프락시움이 끝내지 않는다. 정식 인증을 만들 때 다시 본다 |

## 11. 새 부품을 만들 때 읽을 가이드

대화 상자, 폼, 표, 탭을 새로 만들 때만 가이드 저장소의 해당 가이드를 연다: `guides/ui-components/dialog`, `guides/forms/forms`, `guides/ui-atoms/responsive-table`, `guides/ui-behaviors/sortable-table`, `guides/ui-components/tabs`, `guides/forms/ime-safe-enter-submit`, `guides/accessibility/accessible-error-announcement`. 가이드는 웹에서 읽기만 한다. 상류가 바뀌어 이 문서가 낡았는지는 분기마다 사람이 확인한다.

## 출처와 라이선스

이 문서는 [GoogleChrome/modern-web-guidance-src](https://github.com/GoogleChrome/modern-web-guidance-src)의 `guides/`(커밋 5e5cd33)를 바탕으로 프락시움에 맞게 추리고 한국어로 요약했다. 가이드 본문은 [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) 라이선스이다. 바꾼 점: 252개 가이드 가운데 프락시움에 해당하는 것만 골랐고, 문장과 코드를 옮기지 않고 새로 썼으며, 프락시움의 결정(ADR-0035, ADR-0045)과 하네스 검사에 맞춰 규칙을 더하거나 뺐다. 원 저작자가 이 문서를 보증하지 않는다.
