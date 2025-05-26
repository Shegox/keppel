INSERT INTO accounts (name, auth_tenant_id, next_storage_sweep_at) VALUES ('test1', 'test1authtenant', 25200);

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (6, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e', 1048919, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (3, 'test1', 'sha256:92b29e540b6fcadd4e07525af1546c7eff1bb9a8ef0ef249e0b234cdb13dbea3', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (4, 'test1', 'sha256:eb56d5d5d6a0b061ca49785b5a29e899e68208cdb87853f150e97ecb90d17d92', 1048919, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (5, 'test1', 'sha256:e737c274a038006cd0423ea3526c5c154e025ca2d47c544f54f5a88ee8ac2a94', 1048919, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (6, 'test1', 'sha256:804845712601c0fff29e63faaa1804fd15f18bd6206a5a6d3f0c1c78c628eb2d', 1412, 'e7f6c011776e8db7cd330b54174fd76f7d0216b612387a5ffcfb81e6f0919683', 3600, 'application/vnd.docker.container.image.v1+json', 608400);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223', 4);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223', 5);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223', 6);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 3);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223', '{"config":{"digest":"sha256:804845712601c0fff29e63faaa1804fd15f18bd6206a5a6d3f0c1c78c628eb2d","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:eb56d5d5d6a0b061ca49785b5a29e899e68208cdb87853f150e97ecb90d17d92","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:e737c274a038006cd0423ea3526c5c154e025ca2d47c544f54f5a88ee8ac2a94","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:b0ab79e83bdb2090b5b78f523b6d88272b1a68bdbae8b3705dad0a487ba65d17', '{"manifests":[{"digest":"sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"amd64","os":"linux"},"size":592},{"digest":"sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"arm","os":"linux"},"size":592}],"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', '{"config":{"digest":"sha256:92b29e540b6fcadd4e07525af1546c7eff1bb9a8ef0ef249e0b234cdb13dbea3","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:b0ab79e83bdb2090b5b78f523b6d88272b1a68bdbae8b3705dad0a487ba65d17', 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223');
INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:b0ab79e83bdb2090b5b78f523b6d88272b1a68bdbae8b3705dad0a487ba65d17', 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223', 'application/vnd.docker.distribution.manifest.v2+json', 2099842, 3600, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:b0ab79e83bdb2090b5b78f523b6d88272b1a68bdbae8b3705dad0a487ba65d17', 'application/vnd.docker.distribution.manifest.list.v2+json', 4200211, 3600, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 'application/vnd.docker.distribution.manifest.v2+json', 2099842, 3600, 1, 1, 90000);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at, has_enriched_report) VALUES (1, 'sha256:207a16511ab28a6c3ff0ad6e483ba79fb59a9ebf3721c94e4b91b825bfecf223', 'Clean', '', 3600, TRUE);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:b0ab79e83bdb2090b5b78f523b6d88272b1a68bdbae8b3705dad0a487ba65d17', 'Pending', '', 3600);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at, has_enriched_report) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 'Clean', '', 3600, TRUE);
