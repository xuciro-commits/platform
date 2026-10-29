import { useReadQuery } from "@platform/app";

export function useDirectInstall(): boolean {
  const profile = useReadQuery<{ profile: string; directInstall: boolean }>("/v1/release-profile");
  return !profile.isError && profile.data?.directInstall === true;
}
