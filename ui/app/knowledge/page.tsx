import { Suspense } from "react";
import KnowledgeScreen from "@/components/KnowledgeScreen";

// The knowledge screen reads the `?kb=` query param via useSearchParams, which
// must be wrapped in Suspense for the static export build.
export default function Knowledge() {
  return (
    <Suspense fallback={null}>
      <KnowledgeScreen />
    </Suspense>
  );
}
