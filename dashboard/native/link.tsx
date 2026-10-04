import type { AnchorHTMLAttributes } from "react";
import { navigate } from "./navigation";

export default function Link({ href, onClick, children, ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) {
  return <a href={href} {...props} onClick={(event) => {
    onClick?.(event);
    if (!event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && !props.target && href.startsWith("/")) {
      event.preventDefault(); navigate(href);
    }
  }}>{children}</a>;
}
