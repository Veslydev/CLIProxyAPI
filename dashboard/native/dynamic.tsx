import { lazy, Suspense, type ReactNode } from "react";

export default function dynamic<P extends object>(loader: () => Promise<{ default: (props: P) => ReactNode }>, options?: { ssr?: boolean; loading?: () => ReactNode }) {
  const Component = lazy(loader);
  return function Dynamic(props: P) {
    return <Suspense fallback={options?.loading?.() ?? null}><Component {...props} /></Suspense>;
  };
}
