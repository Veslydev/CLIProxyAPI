-- CreateTable
CREATE TABLE "CustomProvider" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "providerId" TEXT NOT NULL,
    "baseUrl" TEXT NOT NULL,
    "apiKeyHash" TEXT NOT NULL,
    "prefix" TEXT,
    "proxyUrl" TEXT,
    "headers" JSONB NOT NULL DEFAULT '{}',
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "CustomProvider_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "CustomProviderModel" (
    "id" TEXT NOT NULL,
    "customProviderId" TEXT NOT NULL,
    "upstreamName" TEXT NOT NULL,
    "alias" TEXT NOT NULL,

    CONSTRAINT "CustomProviderModel_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "CustomProviderExcludedModel" (
    "id" TEXT NOT NULL,
    "customProviderId" TEXT NOT NULL,
    "pattern" TEXT NOT NULL,

    CONSTRAINT "CustomProviderExcludedModel_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "CustomProvider_providerId_key" ON "CustomProvider"("providerId");

-- CreateIndex
CREATE INDEX "CustomProvider_userId_idx" ON "CustomProvider"("userId");

-- CreateIndex
CREATE INDEX "CustomProvider_providerId_idx" ON "CustomProvider"("providerId");

-- CreateIndex
CREATE INDEX "CustomProviderModel_customProviderId_idx" ON "CustomProviderModel"("customProviderId");

-- CreateIndex
CREATE INDEX "CustomProviderExcludedModel_customProviderId_idx" ON "CustomProviderExcludedModel"("customProviderId");

-- AddForeignKey
ALTER TABLE "CustomProvider" ADD CONSTRAINT "CustomProvider_userId_fkey" FOREIGN KEY ("userId") REFERENCES "users"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "CustomProviderModel" ADD CONSTRAINT "CustomProviderModel_customProviderId_fkey" FOREIGN KEY ("customProviderId") REFERENCES "CustomProvider"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "CustomProviderExcludedModel" ADD CONSTRAINT "CustomProviderExcludedModel_customProviderId_fkey" FOREIGN KEY ("customProviderId") REFERENCES "CustomProvider"("id") ON DELETE CASCADE ON UPDATE CASCADE;
