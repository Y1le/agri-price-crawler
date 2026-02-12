CREATE TABLE IF NOT EXISTS `prices` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `firstCateId` BIGINT UNSIGNED NOT NULL COMMENT '一级品类ID',
  `secondCateId` BIGINT UNSIGNED NOT NULL COMMENT '二级品类ID',
  `cateId` BIGINT UNSIGNED NOT NULL COMMENT '品类ID',
  `cateName` VARCHAR(100) NOT NULL COMMENT '品类名称',
  `breedId` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '品种ID',
  `breedName` VARCHAR(100) NOT NULL COMMENT '品种名称',
  `minPrice` DECIMAL(10,2) NOT NULL COMMENT '最低价',
  `maxPrice` DECIMAL(10,2) NOT NULL COMMENT '最高价',
  `avgPrice` DECIMAL(10,2) NOT NULL COMMENT '平均价',
  `weightingAvgPrice` DECIMAL(10,2) NOT NULL COMMENT '加权平均价',
  `upDownPrice` DECIMAL(10,2) NOT NULL COMMENT '涨跌额',
  `increase` DECIMAL(10,4) NOT NULL COMMENT '涨幅',
  `unit` VARCHAR(20) NOT NULL COMMENT '单位',
  `addressDetail` VARCHAR(200) NOT NULL COMMENT '详细地址',
  `provinceId` INT UNSIGNED NOT NULL COMMENT '省份ID',
  `cityId` INT UNSIGNED NOT NULL COMMENT '城市ID',
  `areaId` INT UNSIGNED NOT NULL COMMENT '区域ID',
  `createdAt` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updatedAt` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  `statisNum` INT UNSIGNED NOT NULL COMMENT '统计数量',
  `sourceType` VARCHAR(20) NOT NULL COMMENT '来源类型(supply/demand)', 
  `trend` TINYINT NOT NULL COMMENT '趋势(1:涨, -1:跌, 0:平)',
  `traceId` VARCHAR(64) NOT NULL COMMENT '链路追踪ID',
  PRIMARY KEY (`id`),
  KEY `idxCollectDate` (`collectDate`),
  KEY `idxCateId` (`cateId`),
  KEY `idxArea` (`provinceId`, `cityId`, `areaId`),
  KEY `idxCreateTime` (`createdAt`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='农产品价格数据表';

DROP TABLE IF EXISTS `subscribe`;

CREATE TABLE `subscribe` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `instanceID` VARCHAR(32) DEFAULT NULL,
  `name` varchar(45) NOT NULL,
  `email` VARCHAR(256) NOT NULL,                  
  `city` VARCHAR(256) NOT NULL,
  `extendShadow` longtext DEFAULT NULL,
  `createdAt` timestamp NOT NULL DEFAULT current_timestamp(),
  `updatedAt` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  PRIMARY KEY (`id`),
  UNIQUE KEY `instanceID_UNIQUE` (`instanceID`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订阅表';

DROP TABLE IF EXISTS `user`;

CREATE TABLE `user` (
  `id` bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT,
  `instanceID` varchar(32) DEFAULT NULL,
  `name` varchar(45) NOT NULL,
  `status` int(1) DEFAULT 1 COMMENT '1:可用，0:不可用',
  `nickname` varchar(30) NOT NULL,
  `password` varchar(255) NOT NULL,
  `email` varchar(256) NOT NULL,
  `phone` varchar(20) DEFAULT NULL,
  `isAdmin` tinyint(1) UNSIGNED NOT NULL DEFAULT 0 COMMENT '1: administrator\\\\n0: non-administrator',
  `extendShadow` longtext DEFAULT NULL,
  `loginedAt` timestamp NULL DEFAULT NULL COMMENT 'last login time',
  `createdAt` timestamp NOT NULL DEFAULT current_timestamp(),
  `updatedAt` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_name` (`name`),
  UNIQUE KEY `instanceID_UNIQUE` (`instanceID`)
) ENGINE=InnoDB AUTO_INCREMENT=38 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';