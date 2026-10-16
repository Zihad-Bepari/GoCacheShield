<?php

namespace GoCacheShield\Exceptions;

use Exception;

class TooManyLoadsException extends Exception
{
    public function __construct(string $message = "Concurrent load limit exceeded")
    {
        parent::__construct($message);
    }
}
