"use client";
import dynamic from "next/dynamic";
import { ConsoleLoading } from "../../../components/console-loading";
const Client = dynamic(
  () => import("@reign/web-client").then((m) => m.RemoteApp),
  { ssr: false, loading: ConsoleLoading },
);
export default function Chat() {
  return <Client />;
}
