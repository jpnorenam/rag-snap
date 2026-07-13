import type { Metadata } from "next";
import AppShell from "@/components/AppShell";
import "./globals.scss";

export const metadata: Metadata = {
  title: "RAG",
  description: "Chat with your local knowledge bases",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}
