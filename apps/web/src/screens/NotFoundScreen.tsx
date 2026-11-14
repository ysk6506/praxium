import { routeHash } from "../data";
import { ButtonLink, EmptyState, PageHeader } from "../ui";

/** An address that names no screen. The path is shown as it was written, so a typing mistake can be seen. */
export function NotFoundScreen({ path }: { readonly path: string }) {
  return (
    <>
      <PageHeader title="페이지를 찾을 수 없습니다" />
      <EmptyState
        title="이 주소에는 화면이 없습니다"
        action={
          <ButtonLink variant="primary" href={routeHash({ page: "kgs" })}>
            KG 목록으로
          </ButtonLink>
        }
      >
        주소 #{path}에 해당하는 화면이 없습니다. 주소를 잘못 입력했거나 링크가 잘린 것일 수
        있습니다.
      </EmptyState>
    </>
  );
}
