# Configuration Guide

## Environment Variables

This application supports loading configuration from environment variables. Sensitive information should be stored in environment variables instead of config files.

## Environment Variable Naming Convention

The application follows this naming convention for environment variables:
`BASENAME_FIELD_GROUP_SUBFIELD`

Where `BASENAME` is `CRAW` for this application.

## Supported Environment Variables

### Database Configuration
- `CRAW_MYSQL_HOST` - MySQL host and port (default: 127.0.0.1:3306)
- `CRAW_MYSQL_USERNAME` - MySQL username
- `CRAW_MYSQL_PASSWORD` - MySQL password
- `CRAW_MYSQL_DATABASE` - MySQL database name
- `CRAW_MYSQL_MAX_IDLE_CONNECTIONS` - Maximum idle connections
- `CRAW_MYSQL_MAX_OPEN_CONNECTIONS` - Maximum open connections
- `CRAW_MYSQL_LOG_LEVEL` - Logging level for MySQL operations

### Redis Configuration
- `CRAW_REDIS_HOST` - Redis host (default: 127.0.0.1)
- `CRAW_REDIS_PORT` - Redis port (default: 6379)
- `CRAW_REDIS_PASSWORD` - Redis password
- `CRAW_REDIS_USERNAME` - Redis username
- `CRAW_REDIS_DATABASE` - Redis database number

### JWT Configuration
- `CRAW_JWT_REALM` - JWT realm name (default: JWT)
- `CRAW_JWT_KEY` - JWT signing key (required, should be kept secret)
- `CRAW_JWT_TIMEOUT` - JWT token timeout (default: 24h)
- `CRAW_JWT_MAX_REFRESH` - Maximum refresh time (default: 24h)

### Crawler Configuration
- `CRAW_CRAWLER_DEVICE_ID` - Device ID for cnhnb.com API (required)
- `CRAW_CRAWLER_SECRET` - Secret key for cnhnb.com API (required)

### Email Configuration
- `CRAW_EMAIL_HOST` - SMTP server host (default: smtp.qq.com)
- `CRAW_EMAIL_PORT` - SMTP server port (default: 465)
- `CRAW_EMAIL_USERNAME` - SMTP username
- `CRAW_EMAIL_PASSWORD` - SMTP password (required)
- `CRAW_EMAIL_FROM` - Sender email address

### Doubao AI API Configuration
- `CRAW_DOUBAO_API_KEY` - Doubao API key (required)
- `CRAW_DOUBAO_BASE_URL` - Doubao API base URL
- `CRAW_DOUBAO_MODEL` - Doubao model name
- `CRAW_DOUBAO_TIMEOUT_SEC` - Request timeout in seconds
- `CRAW_DOUBAO_MAX_RETRIES` - Maximum retry attempts

## Using the .env File

1. Copy `.env.example` to `.env`:
   ```bash
   cp .env.example .env
   ```

2. Edit `.env` to add your sensitive information:
   ```bash
   # Edit the values in .env file
   CRAW_MYSQL_PASSWORD=your_mysql_password
   CRAW_REDIS_PASSWORD=your_redis_password
   CRAW_JWT_KEY=your_jwt_secret_key
   CRAW_CRAWLER_DEVICE_ID=your_device_id
   CRAW_CRAWLER_SECRET=your_crawler_secret
   CRAW_EMAIL_PASSWORD=your_smtp_password
   CRAW_DOUBAO_API_KEY=your_doubao_api_key
   ```

3. The application will automatically load environment variables from `.env` file on startup.

## Security Best Practices

1. **Never commit .env files** - Ensure `.env` is listed in `.gitignore`
2. **Use strong secrets** - Use sufficiently long and random values for passwords and API keys
3. **Environment-specific values** - Use different values for development, staging, and production environments
4. **Regular rotation** - Regularly rotate API keys and passwords