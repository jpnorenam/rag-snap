import { Suspense } from "react";
import Header from "@/components/Header";
import SearchScreen from "@/components/SearchScreen";

// SearchScreen reads the URL via useSearchParams, which a statically exported
// page must wrap in a Suspense boundary (next build fails without one). The
// Header sits outside it so the title never blinks out on first paint.
export default function Search() {
  return (
    <>
      <Header title="Search" />
      <Suspense>
        <SearchScreen />
      </Suspense>
    </>
  );
}
