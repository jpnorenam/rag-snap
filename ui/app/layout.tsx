import type { Metadata } from "next";
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
      <body>{children}</body>
    </html>
  );
}
