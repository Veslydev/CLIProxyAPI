import type { ImgHTMLAttributes } from "react";
import icon from "../src/app/icon.png?inline";

export default function Image({ src, priority: _priority, ...props }: ImgHTMLAttributes<HTMLImageElement> & { priority?: boolean }) {
  return <img {...props} src={src === "/icon.png" ? icon : src} />;
}
